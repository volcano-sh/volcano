/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package api

import (
	"reflect"
	"testing"
)

func TestOrderString(t *testing.T) {
	o := NewOrderMap()
	o.AddIfNotPresent("abc", 100)
	o.AddIfNotPresent("bcd", 101)
	o.AddIfNotPresent("cde", 102)
	o.AddIfNotPresent("def", 103)

	s := o.String()
	if s != "[abc bcd cde def]" {
		t.Fatalf("Expect: %s, Got: %s", "[abc bcd cde def]", s)
	}
}

func TestOrderAdd(t *testing.T) {
	o := NewOrderMap()
	o.AddIfNotPresent("abc", 100)
	o.AddIfNotPresent("bcd", 101)
	o.AddIfNotPresent("cde", 102)
	o.AddIfNotPresent("def", 103)

	if inx := o.Index("abc"); inx != 0 {
		t.Fatalf("Got index: %v, expect: %v", inx, 0)
	}

	if inx := o.Index("bcd"); inx != 1 {
		t.Fatalf("Got index: %v, expect: %v", inx, 3)
	}
	if inx := o.Index("def"); inx != 3 {
		t.Fatalf("Got index: %v, expect: %v", inx, 3)
	}
	o.Delete("cde")
	if inx := o.Index("def"); inx != 2 {
		t.Fatalf("Got index: %v, expect: %v", inx, 2)
	}

	o.Delete("bcd")
	if inx := o.Index("def"); inx != 1 {
		t.Fatalf("Got index: %v, expect: %v", inx, 1)
	}
	o.Delete("def")
	if inx := o.Index("def"); inx != -1 {
		t.Fatalf("Got index: %v, expect: %v", inx, -1)
	}
	o.AddIfNotPresent("def", 102)
	if inx := o.Index("def"); inx != 1 {
		t.Fatalf("Got index: %v, expect: %v", inx, 1)
	}
	o.Delete("abc")
	if inx := o.Index("def"); inx != 0 {
		t.Fatalf("Got index: %v, expect: %v", inx, 0)
	}
	if inx := o.Index("abc"); inx != -1 {
		t.Fatalf("Got index: %v, expect: %v", inx, -1)
	}
}

func TestOrderUpdate(t *testing.T) {
	o := NewOrderMap()
	o.AddIfNotPresent("abc", 100)
	o.AddIfNotPresent("bcd", 101)
	o.AddIfNotPresent("cde", 102)

	if val := o.Get("abc"); val != 100 {
		t.Fatalf("Got value: %v, expect: %v", val, 100)
	}

	if inx := o.Index("bcd"); inx != 1 {
		t.Fatalf("Got index: %v, expect: %v", inx, 1)
	}
	o.Update("bcd", 1000)
	if val := o.Get("bcd"); val != 1000 {
		t.Fatalf("Got value: %v, expect: %v", val, 1000)
	}
	if inx := o.Index("bcd"); inx != 1 {
		t.Fatalf("Got index: %v, expect: %v", inx, 1)
	}

	if inx := o.Index("def"); inx != -1 {
		t.Fatalf("Got index: %v, expect: %v", inx, -1)
	}
	o.Update("def", 2000)
	if inx := o.Index("def"); inx != 3 {
		t.Fatalf("Got index: %v, expect: %v", inx, 3)
	}
	if val := o.Get("def"); val != 2000 {
		t.Fatalf("Got value: %v, expect: %v", val, 2000)
	}
	if o.Len() != 4 {
		t.Fatalf("Got lenth %v, expect %v", o.Len(), 4)
	}
}

func TestOrderClone(t *testing.T) {
	i := NewOrderMap()
	i.AddIfNotPresent("abc", 100)
	i.AddIfNotPresent("bcd", 101)
	i.AddIfNotPresent("cde", 102)

	o := i.Clone()
	if !reflect.DeepEqual(i, o) {
		t.Fatalf("Got %v, expect %v", o, i)
	}

	if o.Len() != 3 {
		t.Fatalf("Got lenth %v, expect %v", o.Len(), 3)
	}

	if val := o.Get("abc"); val != 100 {
		t.Fatalf("Got value: %v, expect: %v", val, 100)
	}

	if inx := o.Index("bcd"); inx != 1 {
		t.Fatalf("Got index: %v, expect: %v", inx, 1)
	}
	o.Update("bcd", 1000)
	if val := o.Get("bcd"); val != 1000 {
		t.Fatalf("Got value: %v, expect: %v", val, 1000)
	}
	if inx := o.Index("bcd"); inx != 1 {
		t.Fatalf("Got index: %v, expect: %v", inx, 1)
	}
	if o.Len() != 3 {
		t.Fatalf("Got lenth %v, expect %v", o.Len(), 3)
	}

	if inx := o.Index("def"); inx != -1 {
		t.Fatalf("Got index: %v, expect: %v", inx, -1)
	}
	o.Update("def", 2000)
	if inx := o.Index("def"); inx != 3 {
		t.Fatalf("Got index: %v, expect: %v", inx, 3)
	}
	if val := o.Get("def"); val != 2000 {
		t.Fatalf("Got value: %v, expect: %v", val, 2000)
	}
	if o.Len() != 4 {
		t.Fatalf("Got lenth %v, expect %v", o.Len(), 4)
	}
}

func TestOrderEqual(t *testing.T) {
	a := NewOrderMap()
	b := NewOrderMap()
	a.AddIfNotPresent("abc", 100)
	b.AddIfNotPresent("abc", 100)

	b1 := a.Equal(b)
	if !reflect.DeepEqual(b1, true) {
		t.Fatalf("Got: %v, expect: %v", b1, true)
	}

	a.AddIfNotPresent("bcd", 101)
	b1 = a.Equal(b)
	if !reflect.DeepEqual(b1, false) {
		t.Fatalf("Got: %v, expect: %v", b1, false)
	}

	b.AddIfNotPresent("bcd", 101)
	b1 = a.Equal(b)
	if !reflect.DeepEqual(b1, true) {
		t.Fatalf("Got: %v, expect: %v", b1, true)
	}

	// different order
	a.AddIfNotPresent("cde", 102)
	a.AddIfNotPresent("def", 103)
	b.AddIfNotPresent("def", 103)
	b.AddIfNotPresent("cde", 102)
	b1 = a.Equal(b)
	if !reflect.DeepEqual(b1, false) {
		t.Fatalf("Got: %v, expect: %v", b1, false)
	}

	// deep equal
	b.Update("cde", 100)
	if flag := a.DeepEqual(b); flag {
		t.Fatalf("Got: %v, expect: %v", flag, false)
	}
}
