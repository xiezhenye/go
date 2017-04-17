// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Portable half of code generator; mainly statements and control flow.

package gc

import (
	"cmd/compile/internal/types"
	"cmd/internal/obj"
	"cmd/internal/src"
	"fmt"
	"strconv"
)

func Sysfunc(name string) *obj.LSym {
	return Linksym(Runtimepkg.Lookup(name))
}

// addrescapes tags node n as having had its address taken
// by "increasing" the "value" of n.Esc to EscHeap.
// Storage is allocated as necessary to allow the address
// to be taken.
func addrescapes(n *Node) {
	switch n.Op {
	// probably a type error already.
	// dump("addrescapes", n);
	default:
		break

	case ONAME:
		if n == nodfp {
			break
		}

		// if this is a tmpname (PAUTO), it was tagged by tmpname as not escaping.
		// on PPARAM it means something different.
		if n.Class == PAUTO && n.Esc == EscNever {
			break
		}

		// If a closure reference escapes, mark the outer variable as escaping.
		if n.IsClosureVar() {
			addrescapes(n.Name.Defn)
			break
		}

		if n.Class != PPARAM && n.Class != PPARAMOUT && n.Class != PAUTO {
			break
		}

		// This is a plain parameter or local variable that needs to move to the heap,
		// but possibly for the function outside the one we're compiling.
		// That is, if we have:
		//
		//	func f(x int) {
		//		func() {
		//			global = &x
		//		}
		//	}
		//
		// then we're analyzing the inner closure but we need to move x to the
		// heap in f, not in the inner closure. Flip over to f before calling moveToHeap.
		oldfn := Curfn
		Curfn = n.Name.Curfn
		if Curfn.Func.Closure != nil && Curfn.Op == OCLOSURE {
			Curfn = Curfn.Func.Closure
		}
		ln := lineno
		lineno = Curfn.Pos
		moveToHeap(n)
		Curfn = oldfn
		lineno = ln

	case OIND, ODOTPTR:
		break

	// ODOTPTR has already been introduced,
	// so these are the non-pointer ODOT and OINDEX.
	// In &x[0], if x is a slice, then x does not
	// escape--the pointer inside x does, but that
	// is always a heap pointer anyway.
	case ODOT, OINDEX, OPAREN, OCONVNOP:
		if !n.Left.Type.IsSlice() {
			addrescapes(n.Left)
		}
	}
}

// isParamStackCopy reports whether this is the on-stack copy of a
// function parameter that moved to the heap.
func (n *Node) isParamStackCopy() bool {
	return n.Op == ONAME && (n.Class == PPARAM || n.Class == PPARAMOUT) && n.Name.Param.Heapaddr != nil
}

// isParamHeapCopy reports whether this is the on-heap copy of
// a function parameter that moved to the heap.
func (n *Node) isParamHeapCopy() bool {
	return n.Op == ONAME && n.Class == PAUTOHEAP && n.Name.Param.Stackcopy != nil
}

// moveToHeap records the parameter or local variable n as moved to the heap.
func moveToHeap(n *Node) {
	if Debug['r'] != 0 {
		Dump("MOVE", n)
	}
	if compiling_runtime {
		yyerror("%v escapes to heap, not allowed in runtime.", n)
	}
	if n.Class == PAUTOHEAP {
		Dump("n", n)
		Fatalf("double move to heap")
	}

	// Allocate a local stack variable to hold the pointer to the heap copy.
	// temp will add it to the function declaration list automatically.
	heapaddr := temp(types.NewPtr(n.Type))
	heapaddr.Sym = lookup("&" + n.Sym.Name)
	heapaddr.Orig.Sym = heapaddr.Sym

	// Unset AutoTemp to persist the &foo variable name through SSA to
	// liveness analysis.
	// TODO(mdempsky/drchase): Cleaner solution?
	heapaddr.Name.SetAutoTemp(false)

	// Parameters have a local stack copy used at function start/end
	// in addition to the copy in the heap that may live longer than
	// the function.
	if n.Class == PPARAM || n.Class == PPARAMOUT {
		if n.Xoffset == BADWIDTH {
			Fatalf("addrescapes before param assignment")
		}

		// We rewrite n below to be a heap variable (indirection of heapaddr).
		// Preserve a copy so we can still write code referring to the original,
		// and substitute that copy into the function declaration list
		// so that analyses of the local (on-stack) variables use it.
		stackcopy := newname(n.Sym)
		stackcopy.SetAddable(false)
		stackcopy.Type = n.Type
		stackcopy.Xoffset = n.Xoffset
		stackcopy.Class = n.Class
		stackcopy.Name.Param.Heapaddr = heapaddr
		if n.Class == PPARAMOUT {
			// Make sure the pointer to the heap copy is kept live throughout the function.
			// The function could panic at any point, and then a defer could recover.
			// Thus, we need the pointer to the heap copy always available so the
			// post-deferreturn code can copy the return value back to the stack.
			// See issue 16095.
			heapaddr.SetIsOutputParamHeapAddr(true)
		}
		n.Name.Param.Stackcopy = stackcopy

		// Substitute the stackcopy into the function variable list so that
		// liveness and other analyses use the underlying stack slot
		// and not the now-pseudo-variable n.
		found := false
		for i, d := range Curfn.Func.Dcl {
			if d == n {
				Curfn.Func.Dcl[i] = stackcopy
				found = true
				break
			}
			// Parameters are before locals, so can stop early.
			// This limits the search even in functions with many local variables.
			if d.Class == PAUTO {
				break
			}
		}
		if !found {
			Fatalf("cannot find %v in local variable list", n)
		}
		Curfn.Func.Dcl = append(Curfn.Func.Dcl, n)
	}

	// Modify n in place so that uses of n now mean indirection of the heapaddr.
	n.Class = PAUTOHEAP
	n.Xoffset = 0
	n.Name.Param.Heapaddr = heapaddr
	n.Esc = EscHeap
	if Debug['m'] != 0 {
		fmt.Printf("%v: moved to heap: %v\n", n.Line(), n)
	}
}

// autotmpname returns the name for an autotmp variable numbered n.
func autotmpname(n int) string {
	// Give each tmp a different name so that they can be registerized.
	// Add a preceding . to avoid clashing with legal names.
	const prefix = ".autotmp_"
	// Start with a buffer big enough to hold a large n.
	b := []byte(prefix + "      ")[:len(prefix)]
	b = strconv.AppendInt(b, int64(n), 10)
	_ = b
	return types.InternString(b)
}

// make a new Node off the books
func tempnamel(pos src.XPos, curfn *Node, nn *Node, t *types.Type) {
	if curfn == nil {
		Fatalf("no curfn for tempname")
	}
	if curfn.Func.Closure != nil && curfn.Op == OCLOSURE {
		Dump("tempname", curfn)
		Fatalf("adding tempname to wrong closure function")
	}
	if t == nil {
		Fatalf("tempname called with nil type")
	}

	s := &types.Sym{
		Name: autotmpname(len(curfn.Func.Dcl)),
		Pkg:  localpkg,
	}
	n := newnamel(pos, s)
	s.Def = asTypesNode(n)
	n.Type = t
	n.Class = PAUTO
	n.Esc = EscNever
	n.Name.Curfn = curfn
	n.Name.SetAutoTemp(true)
	curfn.Func.Dcl = append(curfn.Func.Dcl, n)

	dowidth(t)
	*nn = *n
}

func temp(t *types.Type) *Node {
	var n Node
	tempnamel(lineno, Curfn, &n, t)
	asNode(n.Sym.Def).SetUsed(true)
	return n.Orig
}

func tempAt(pos src.XPos, curfn *Node, t *types.Type) *Node {
	var n Node
	tempnamel(pos, curfn, &n, t)
	asNode(n.Sym.Def).SetUsed(true)
	return n.Orig
}
