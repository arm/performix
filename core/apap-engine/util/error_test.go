// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDisplayErrorStringSlice(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{name: "nil slice", in: nil, want: "none"},
		{name: "empty slice", in: []string{}, want: "none"},
		{name: "single element", in: []string{"foo"}, want: "`foo`"},
		{name: "preserve whitespace", in: []string{" a ", "b "}, want: "` a `, `b `"},
		{name: "special chars", in: []string{"a,b", "c'd", `e"f`}, want: "`a,b`, `c'd`, `e\"f`"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := DisplayErrorStringSlice(tt.in)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestJoinErrors(t *testing.T) {
	testErrA := errors.New("helloA")
	testErrB := errors.New("helloB")
	tests := []struct {
		name string
		errA error
		errB error
		want error
	}{
		{name: "both errors nil", errA: nil, errB: nil, want: nil},
		{name: "errA nil, errB not", errA: nil, errB: testErrB, want: testErrB},
		{name: "errB nil, errA not", errA: testErrA, errB: nil, want: testErrA},
		{name: "both errors non-nil", errA: testErrA, errB: testErrB, want: errors.Join(testErrA, testErrB)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := JoinErrors(tt.errA, tt.errB)
			assert.Equal(t, tt.want, got)
		})
	}
}
