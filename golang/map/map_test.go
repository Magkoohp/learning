package main

import (
	"fmt"
	"sync"
	"testing"
	"unsafe"
)

func TestMapRead(t *testing.T) {
	m := map[int]int{
		1: 1,
	}

	for i := 2; i < 1000; i++ {
		m[i] = i
	}

	m1 := m[1]
	fmt.Println(m1)
}

func TestName(t *testing.T) {
	k := 1
	fmt.Println(unsafe.Pointer(&k))
}

type Student struct {
	name string
}

func TestMapValue(t *testing.T) {
	m := map[string]Student{"people": {"zhoujielun"}}
	m["people2"] = Student{name: "pp"}
	c := m["people"]
	fmt.Println(c)
}

func TestSyncMap(t *testing.T) {
	m := sync.Map{}
	m.Store(1, 1)
	m.Load(1)
}
