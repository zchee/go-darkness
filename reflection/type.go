// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package reflection exports of stdlib reflect package.
package reflection

import (
	"unsafe"
)

// StringHeader is the header for an string type.
type StringHeader struct {
	Data unsafe.Pointer
	Len  int
}

// InterfaceHeader is the header for an interface{} value.
type InterfaceHeader struct {
	Type *rtype         // 8 bytes for the pointer to the actual struct data followed by
	Word unsafe.Pointer // 8 bytes for the pointer to the type information
}

// tflag is used by an rtype to signal what extra type information is
// available in the memory directly following the rtype value.
//
// tflag values must be kept in sync with copies in:
//	cmd/compile/internal/gc/reflect.go
//	cmd/link/internal/ld/decodesym.go
//	runtime/type.go
type tflag uint8

const (
	// TflagUncommon means that there is a pointer, *uncommonType,
	// just beyond the outer type structure.
	//
	// For example, if t.Kind() == Struct and t.tflag&TflagUncommon != 0,
	// then t has uncommonType data and it can be accessed as:
	//
	//	type tUncommon struct {
	//		structType
	//		u uncommonType
	//	}
	//	u := &(*tUncommon)(unsafe.Pointer(t)).u
	TflagUncommon tflag = 1 << 0

	// TflagExtraStar means the name in the str field has an
	// extraneous '*' prefix. This is because for most types T in
	// a program, the type *T also exists and reusing the str data
	// saves binary size.
	TflagExtraStar tflag = 1 << 1

	// TflagNamed means the type has a name.
	TflagNamed tflag = 1 << 2

	// TflagRegularMemory means that equal and hash functions can treat
	// this type as a single region of t.size bytes.
	TflagRegularMemory tflag = 1 << 3
)

type rtype struct {
	size       uintptr
	ptrdata    uintptr // number of bytes in the type that can contain pointers
	hash       uint32  // hash of type; avoids computation in hash tables
	tflag      tflag   // extra type information flags
	align      uint8   // alignment of variable with this type
	fieldAlign uint8   // alignment of struct field with this type
	kind       uint8   // enumeration for C
	// function for comparing objects of this type
	// (ptr to object A, ptr to object B) -> ==?
	equal     func(unsafe.Pointer, unsafe.Pointer) bool
	gcdata    *byte   // garbage collection data
	str       NameOff // string form
	ptrToThis TypeOff // type for pointer to this type, may be zero
}

// StructType represents a struct type.
type StructType struct {
	rtype
	PkgPath Name
	Fields  []StructField // sorted by offset
}

// StructField represents a struct field.
type StructField struct {
	Name        Name    // name is always non-empty
	typ         *rtype  // type of field
	OffsetEmbed uintptr // byte offset of field<<1 | isEmbedded
}

// Add returns p+x.
//
// The whySafe string is ignored, so that the function still inlines
// as efficiently as p+x, but all call sites should use the string to
// record why the addition is safe, which is to say why the addition
// does not cause x to advance to the very end of p's allocation
// and therefore point incorrectly at the next block in memory.
func Add(p unsafe.Pointer, x uintptr, whySafe string) unsafe.Pointer {
	return unsafe.Pointer(uintptr(p) + x)
}

// Name is an encoded type Name with optional extra data.
//
// The first byte is a bit field containing:
//
//	1<<0 the Name is exported
//	1<<1 tag data follows the Name
//	1<<2 pkgPath nameOff follows the Name and tag
//
// The next two bytes are the data length:
//
//	 l := uint16(data[1])<<8 | uint16(data[2])
//
// Bytes [3:3+l] are the string data.
//
// If tag data follows then bytes 3+l and 3+l+1 are the tag length,
// with the data following.
//
// If the import path follows, then 4 bytes at the end of
// the data form a nameOff. The import path is only set for concrete
// methods that are defined in a different package than their type.
//
// If a Name starts with "*", then the exported bit represents
// whether the pointed to type is exported.
type Name struct {
	bytes *byte
}

func (n Name) Data(off int, whySafe string) *byte {
	return (*byte)(Add(unsafe.Pointer(n.bytes), uintptr(off), whySafe))
}

func (n Name) IsExported() bool {
	return (*n.bytes)&(1<<0) != 0
}

func (n Name) NameLen() int {
	return int(uint16(*n.Data(1, "name len field"))<<8 | uint16(*n.Data(2, "name len field")))
}

func (n Name) TagLen() int {
	if *n.Data(0, "name flag field")&(1<<1) == 0 {
		return 0
	}
	off := 3 + n.NameLen()
	return int(uint16(*n.Data(off, "name taglen field"))<<8 | uint16(*n.Data(off+1, "name taglen field")))
}

func (n Name) Name() (s string) {
	if n.bytes == nil {
		return
	}
	b := (*[4]byte)(unsafe.Pointer(n.bytes))

	hdr := (*StringHeader)(unsafe.Pointer(&s))
	hdr.Data = unsafe.Pointer(&b[3])
	hdr.Len = int(b[1])<<8 | int(b[2])
	return s
}

func (n Name) Tag() (s string) {
	tl := n.TagLen()
	if tl == 0 {
		return ""
	}
	nl := n.NameLen()
	hdr := (*StringHeader)(unsafe.Pointer(&s))
	hdr.Data = unsafe.Pointer(n.Data(3+nl+2, "non-empty string"))
	hdr.Len = tl
	return s
}

func (n Name) PkgPath() string {
	if n.bytes == nil || *n.Data(0, "name flag field")&(1<<2) == 0 {
		return ""
	}
	off := 3 + n.NameLen()
	if tl := n.TagLen(); tl > 0 {
		off += 2 + tl
	}
	var nameOff int32
	// Note that this field may not be aligned in memory,
	// so we cannot use a direct int32 assignment here.
	copy((*[4]byte)(unsafe.Pointer(&nameOff))[:], (*[4]byte)(unsafe.Pointer(n.Data(off, "name offset field")))[:])
	pkgPathName := Name{(*byte)(ResolveTypeOff(unsafe.Pointer(n.bytes), nameOff))}
	return pkgPathName.Name()
}

func NewName(n, tag string, exported bool) Name {
	if len(n) > 1<<16-1 {
		panic("reflect.nameFrom: name too long: " + n)
	}
	if len(tag) > 1<<16-1 {
		panic("reflect.nameFrom: tag too long: " + tag)
	}

	var bits byte
	l := 1 + 2 + len(n)
	if exported {
		bits |= 1 << 0
	}
	if len(tag) > 0 {
		l += 2 + len(tag)
		bits |= 1 << 1
	}

	b := make([]byte, l)
	b[0] = bits
	b[1] = uint8(len(n) >> 8)
	b[2] = uint8(len(n))
	copy(b[3:], n)
	if len(tag) > 0 {
		tb := b[3+len(n):]
		tb[0] = uint8(len(tag) >> 8)
		tb[1] = uint8(len(tag))
		copy(tb[2:], tag)
	}

	return Name{bytes: &b[0]}
}

//go:linkname ResolveNameOff reflect.resolveNameOff

// ResolveNameOff resolves a name offset from a base pointer.
// The (*rtype).nameOff method is a convenience wrapper for this function.
// Implemented in the runtime package.
func ResolveNameOff(ptrInModule unsafe.Pointer, off int32) unsafe.Pointer

//go:linkname ResolveTypeOff reflect.resolveTypeOff

// ResolveTypeOff resolves an *rtype offset from a base type.
// The (*rtype).typeOff method is a convenience wrapper for this function.
// Implemented in the runtime package.
func ResolveTypeOff(rtype unsafe.Pointer, off int32) unsafe.Pointer

//go:linkname ResolveTextOff reflect.resolveTextOff

// ResolveTextOff resolves a function pointer offset from a base type.
// The (*rtype).textOff method is a convenience wrapper for this function.
// Implemented in the runtime package.
func ResolveTextOff(rtype unsafe.Pointer, off int32) unsafe.Pointer

type NameOff int32 // offset to a name
type TypeOff int32 // offset to an *rtype
type TextOff int32 // offset from top of text section

func (t *rtype) NameOff(off NameOff) Name {
	return Name{(*byte)(ResolveNameOff(unsafe.Pointer(t), int32(off)))}
}

func (t *rtype) TypeOff(off TypeOff) *rtype {
	return (*rtype)(ResolveTypeOff(unsafe.Pointer(t), int32(off)))
}

func (t *rtype) TextOff(off TextOff) unsafe.Pointer {
	return ResolveTextOff(unsafe.Pointer(t), int32(off))
}
