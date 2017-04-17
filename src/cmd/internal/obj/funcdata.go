// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package obj

// This file defines the IDs for PCDATA and FUNCDATA instructions
// in Go binaries.
//
// These must agree with ../../../runtime/funcdata.h and
// ../../../runtime/symtab.go.

const (
	PCDATA_StackMapIndex       = 0
	PCDATA_InlTreeIndex        = 1
	FUNCDATA_ArgsPointerMaps   = 0
	FUNCDATA_LocalsPointerMaps = 1
	FUNCDATA_InlTree           = 2

	// ArgsSizeUnknown is set in Func.argsize to mark all functions
	// whose argument size is unknown (C vararg functions, and
	// assembly code without an explicit specification).
	// This value is generated by the compiler, assembler, or linker.
	ArgsSizeUnknown = -0x80000000
)
