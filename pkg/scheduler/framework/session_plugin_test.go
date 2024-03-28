package framework

import (
	"errors"
	"reflect"
	"testing"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
)

func TestGenericPredicate(t *testing.T) {
	t1 := "integer larger-than-5 and string is volcano"
	enable := true
	ssn := Session{
		Tiers: []conf.Tier{
			{
				Plugins: []conf.PluginOption{{
					Name:             t1,
					EnabledPredicate: &enable,
				}},
			},
		},
		genericPredicateFns: map[string]api.GenericPredicateFn{
			t1: func(i ...interface{}) *api.GenericResult {
				if res := api.GenericParamCheck([]reflect.Type{
					reflect.TypeOf(int(0)),
					reflect.TypeOf(string("")),
				}, i...); !res.Success() {
					return res
				}

				v := i[0].(int)
				if v <= 5 {
					return api.NewGenericResult(api.GenericFailed, "smaller than 5")
				}
				s := i[1].(string)
				if s != "volcano" {
					return api.NewGenericResult(api.GenericFailed, "not volcano")
				}
				return api.NewGenericResult(api.GenericSuccess)
			},
		},
	}
	tests := []struct {
		name      string
		expect    error
		parameter []interface{}
	}{
		{
			name:      "no parameter should be true",
			expect:    nil,
			parameter: []interface{}{},
		},
		{
			name:      "parameter number not match(less) should be true",
			expect:    nil,
			parameter: []interface{}{1},
		},
		{
			name:      "parameter number not match(more) should be true",
			expect:    nil,
			parameter: []interface{}{1, 2, 3},
		},
		{
			name:      "parameter number match but type mismatch should be also true",
			expect:    nil,
			parameter: []interface{}{1, 2},
		},
		{
			name:      "parameters are legal but failed the first predication",
			expect:    errors.New("smaller than 5"),
			parameter: []interface{}{1, "volcano"},
		},
		{
			name:      "first parameter success but failed the second one",
			expect:    errors.New("not volcano"),
			parameter: []interface{}{6, "mountain"},
		},
		{
			name:      "all success should also be true",
			expect:    nil,
			parameter: []interface{}{6, "volcano"},
		},
	}
	for _, s := range tests {
		err := ssn.GenericPredicateFn(s.parameter...)
		if s.expect == nil && err == nil {
			continue
		} else if (s.expect == nil && err != nil) || (s.expect != nil && err == nil) {
			t.Errorf("case %s failed expect: %v actual: %v", s.name, s.expect, err)
		} else if s.expect.Error() != err.Error() {
			t.Errorf("case %s failed expect: %v actual: %v", s.name, s.expect, err)
		}
	}
}

func TestGenericOrder(t *testing.T) {
	p1 := "vote based on priority"
	p2 := "vote based on reversed priority"
	enable := true
	type task struct {
		name     string
		priority int
	}
	sess := Session{
		Tiers: []conf.Tier{
			{
				Plugins: []conf.PluginOption{{
					Name:             p1,
					EnabledNodeOrder: &enable,
				}, {
					Name:             p2,
					EnabledNodeOrder: &enable,
				}},
			},
		},
		genericOrderFns: map[string]api.GenericOrderFn{
			p1: func(i ...interface{}) (float64, *api.GenericResult) {
				if res := api.GenericParamCheck([]reflect.Type{
					reflect.TypeOf(&task{}),
				}, i...); !res.Success() {
					return 0.0, res
				}

				tsk := i[0].(*task)
				if tsk.priority > 1 {
					return 10.0, api.NewGenericResult(api.GenericSuccess)
				}
				return 90.0, api.NewGenericResult(api.GenericSuccess)
			},
			p2: func(i ...interface{}) (float64, *api.GenericResult) {
				if res := api.GenericParamCheck([]reflect.Type{
					reflect.TypeOf(&task{}),
				}, i...); !res.Success() {
					return 0.0, res
				}

				tsk := i[0].(*task)
				if tsk.priority > 1 {
					return 90.0, api.NewGenericResult(api.GenericSuccess)
				}
				return 10.0, api.NewGenericResult(api.GenericSuccess)
			},
		},
	}
	type expect struct {
		score float64
		err   error
	}

	tests := []struct {
		name      string
		expect    expect
		parameter []interface{}
	}{
		{
			name:      "no parameter should be true",
			expect:    expect{score: 0.0, err: nil},
			parameter: []interface{}{},
		},
		{
			name:      "parameter number not match(more) should be true",
			expect:    expect{score: 0.0, err: nil},
			parameter: []interface{}{1, 2, 3},
		},
		{
			name:      "parameter number match but type(int) mismatch shoulde be also true",
			expect:    expect{score: 0.0, err: nil},
			parameter: []interface{}{1},
		},
		{
			name:      "parameter number match but type(string) mismatch shoulde be also true",
			expect:    expect{score: 0.0, err: nil},
			parameter: []interface{}{"abc"},
		},
		{
			name:      "all success should also be true",
			expect:    expect{score: 100.0, err: nil},
			parameter: []interface{}{&task{name: "t1", priority: 3}},
		},
	}
	for _, s := range tests {
		score, err := sess.GenericOrderFn(s.parameter...)
		t.Logf("score: %f", score)
		if s.expect.err == nil && err == nil {
			continue
		} else if (s.expect.err == nil && err != nil) || (s.expect.err != nil && err == nil) {
			t.Errorf("case %s failed expect: %v actual: %v", s.name, s.expect, err)
		} else if s.expect.err.Error() != err.Error() {
			t.Errorf("case %s failed expect: %v actual: %v", s.name, s.expect, err)
		} else if s.expect.score != score {
			t.Errorf("case %s failed expect: %v actual: %v", s.name, s.expect, err)
		}
	}
}
