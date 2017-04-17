// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ppc64

import (
	"cmd/compile/internal/gc"
	"cmd/internal/obj"
	"cmd/internal/obj/ppc64"
)

func defframe(pp *gc.Progs, fn *gc.Node, sz int64) {
	// fill in argument size, stack size
	pp.Text.To.Type = obj.TYPE_TEXTSIZE

	pp.Text.To.Val = int32(gc.Rnd(fn.Type.ArgWidth(), int64(gc.Widthptr)))
	frame := uint32(gc.Rnd(sz, int64(gc.Widthreg)))
	pp.Text.To.Offset = int64(frame)

	// insert code to zero ambiguously live variables
	// so that the garbage collector only sees initialized values
	// when it looks for pointers.
	p := pp.Text

	hi := int64(0)
	lo := hi

	// iterate through declarations - they are sorted in decreasing xoffset order.
	for _, n := range fn.Func.Dcl {
		if !n.Name.Needzero() {
			continue
		}
		if n.Class != gc.PAUTO {
			gc.Fatalf("needzero class %d", n.Class)
		}
		if n.Type.Width%int64(gc.Widthptr) != 0 || n.Xoffset%int64(gc.Widthptr) != 0 || n.Type.Width == 0 {
			gc.Fatalf("var %L has size %d offset %d", n, int(n.Type.Width), int(n.Xoffset))
		}

		if lo != hi && n.Xoffset+n.Type.Width >= lo-int64(2*gc.Widthreg) {
			// merge with range we already have
			lo = n.Xoffset

			continue
		}

		// zero old range
		p = zerorange(pp, p, int64(frame), lo, hi)

		// set new range
		hi = n.Xoffset + n.Type.Width

		lo = n.Xoffset
	}

	// zero final range
	zerorange(pp, p, int64(frame), lo, hi)
}

func zerorange(pp *gc.Progs, p *obj.Prog, frame int64, lo int64, hi int64) *obj.Prog {
	cnt := hi - lo
	if cnt == 0 {
		return p
	}
	if cnt < int64(4*gc.Widthptr) {
		for i := int64(0); i < cnt; i += int64(gc.Widthptr) {
			p = pp.Appendpp(p, ppc64.AMOVD, obj.TYPE_REG, ppc64.REGZERO, 0, obj.TYPE_MEM, ppc64.REGSP, gc.Ctxt.FixedFrameSize()+frame+lo+i)
		}
	} else if cnt <= int64(128*gc.Widthptr) {
		p = pp.Appendpp(p, ppc64.AADD, obj.TYPE_CONST, 0, gc.Ctxt.FixedFrameSize()+frame+lo-8, obj.TYPE_REG, ppc64.REGRT1, 0)
		p.Reg = ppc64.REGSP
		p = pp.Appendpp(p, obj.ADUFFZERO, obj.TYPE_NONE, 0, 0, obj.TYPE_MEM, 0, 0)
		p.To.Name = obj.NAME_EXTERN
		p.To.Sym = gc.Duffzero
		p.To.Offset = 4 * (128 - cnt/int64(gc.Widthptr))
	} else {
		p = pp.Appendpp(p, ppc64.AMOVD, obj.TYPE_CONST, 0, gc.Ctxt.FixedFrameSize()+frame+lo-8, obj.TYPE_REG, ppc64.REGTMP, 0)
		p = pp.Appendpp(p, ppc64.AADD, obj.TYPE_REG, ppc64.REGTMP, 0, obj.TYPE_REG, ppc64.REGRT1, 0)
		p.Reg = ppc64.REGSP
		p = pp.Appendpp(p, ppc64.AMOVD, obj.TYPE_CONST, 0, cnt, obj.TYPE_REG, ppc64.REGTMP, 0)
		p = pp.Appendpp(p, ppc64.AADD, obj.TYPE_REG, ppc64.REGTMP, 0, obj.TYPE_REG, ppc64.REGRT2, 0)
		p.Reg = ppc64.REGRT1
		p = pp.Appendpp(p, ppc64.AMOVDU, obj.TYPE_REG, ppc64.REGZERO, 0, obj.TYPE_MEM, ppc64.REGRT1, int64(gc.Widthptr))
		p1 := p
		p = pp.Appendpp(p, ppc64.ACMP, obj.TYPE_REG, ppc64.REGRT1, 0, obj.TYPE_REG, ppc64.REGRT2, 0)
		p = pp.Appendpp(p, ppc64.ABNE, obj.TYPE_NONE, 0, 0, obj.TYPE_BRANCH, 0, 0)
		gc.Patch(p, p1)
	}

	return p
}

func ginsnop(pp *gc.Progs) {
	p := pp.Prog(ppc64.AOR)
	p.From.Type = obj.TYPE_REG
	p.From.Reg = ppc64.REG_R0
	p.To.Type = obj.TYPE_REG
	p.To.Reg = ppc64.REG_R0
}

func ginsnop2(pp *gc.Progs) {
	// PPC64 is unusual because TWO nops are required
	// (see gc/cgen.go, gc/plive.go -- copy of comment below)
	//
	// On ppc64, when compiling Go into position
	// independent code on ppc64le we insert an
	// instruction to reload the TOC pointer from the
	// stack as well. See the long comment near
	// jmpdefer in runtime/asm_ppc64.s for why.
	// If the MOVD is not needed, insert a hardware NOP
	// so that the same number of instructions are used
	// on ppc64 in both shared and non-shared modes.

	ginsnop(pp)
	if gc.Ctxt.Flag_shared {
		p := pp.Prog(ppc64.AMOVD)
		p.From.Type = obj.TYPE_MEM
		p.From.Offset = 24
		p.From.Reg = ppc64.REGSP
		p.To.Type = obj.TYPE_REG
		p.To.Reg = ppc64.REG_R2
	} else {
		ginsnop(pp)
	}
}
