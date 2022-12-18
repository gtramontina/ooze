package faketestingt

import (
	"reflect"
	"testing"
	"unsafe"
)

type SubTest struct {
	subtest func(*testing.T)
	t       *testing.T
	tElem   reflect.Value
	common  reflect.Value
}

func NewSubTest(subtest func(*testing.T)) *SubTest {
	t := new(testing.T)
	tElem := reflect.ValueOf(t).Elem()
	common := tElem.FieldByName("common")
	parentElem := common.FieldByName("parent")
	parentElem = reflect.NewAt(parentElem.Type(), unsafe.Pointer(parentElem.UnsafeAddr())).Elem()
	parentElem.Set(reflect.New(common.Type()))

	return &SubTest{
		subtest: subtest,
		t:       t,
		tElem:   tElem,
		common:  common,
	}
}

func (s *SubTest) Run() {
	s.subtest(s.t)
}

func (s *SubTest) IsParallel() bool {
	return s.tElem.FieldByName("isParallel").Bool()
}

func (s *SubTest) Failed() bool {
	return s.t.Failed()
}
