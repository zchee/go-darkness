// Copyright 2020 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build ignore

package main

import (
	"fmt"
	"unsafe"

	"github.com/zchee/go-darkness/reflection"
)

type example struct {
	A int    `json:"a,omitempty"`
	B int    `json:"b,omitempty"`
	C string `protobuf:"varint,1,opt,name=c,proto3" json:"c,omitempty" yaml:"c,omitempty"`
}

func main() {
	a := example{
		A: 1,
		B: 2,
		C: "test string in C",
	}

	var iface interface{} = a

	ifaceHeader := (*reflection.InterfaceHeader)(unsafe.Pointer(&iface))
	st := (*reflection.StructType)(unsafe.Pointer(ifaceHeader.Type))

	// possible to iterate through the fields
	for i := range st.Fields {
		f := st.Fields[i]
		fmt.Printf("Name: %s, Tag: %8s\n", f.Name.Name(), f.Name.Tag())
	}
}
