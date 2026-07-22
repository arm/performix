// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package recipe

import (
	"strconv"
)

type CountedFlag[T any] struct {
	value  T
	typ    string
	count  int
	parse  func(string) (T, error)
	format func(T) string
}

func NewCountedFlag[T any](
	value T,
	typ string,
	parse func(string) (T, error),
	format func(T) string,
) CountedFlag[T] {
	return CountedFlag[T]{
		value:  value,
		typ:    typ,
		parse:  parse,
		format: format,
	}
}

func NewCountedInt64Flag(defaultValue int64) CountedFlag[int64] {
	return NewCountedFlag[int64](
		defaultValue,
		"int64",
		func(raw string) (int64, error) {
			return strconv.ParseInt(raw, 10, 64)
		},
		func(value int64) string {
			return strconv.FormatInt(value, 10)
		},
	)
}

func (f *CountedFlag[T]) Set(raw string) error {
	pid, err := f.parse(raw)
	if err != nil {
		return err
	}

	f.value = pid
	f.count++
	return nil
}

func (f *CountedFlag[T]) Type() string {
	return f.typ
}

func (f *CountedFlag[T]) String() string {
	return f.format(f.value)
}

func (f *CountedFlag[T]) Repeated() bool {
	return f.count > 1
}

func (f *CountedFlag[T]) Count() int {
	return f.count
}

func (f *CountedFlag[T]) Value() T {
	return f.value
}
