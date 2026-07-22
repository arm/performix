// SPDX-FileCopyrightText: Copyright 2026 Arm Limited and/or its affiliates <open-source-office@arm.com>
// SPDX-License-Identifier: Apache-2.0

package util

func Filter[T any](array []T, predicate func(i int) bool) (result []T) {
	for i := range array {
		if predicate(i) {
			result = append(result, array[i])
		}
	}
	return
}

// Remove the first element which produces a "true" result from the predicate
func RemoveFirst[T any](array []T, predicate func(i int) bool) []T {
	for i := range array {
		if predicate(i) {
			return append(array[:i], array[i+1:]...)
		}
	}
	return array
}

// Remove the element from array at index i
func RemoveAt[T any](array []T, i int) []T {
	return append(array[:i], array[i+1:]...)
}

func Map[T, U any](array []T, f func(v T) U) (result []U) {
	result = make([]U, len(array))
	for i := range array {
		result[i] = f(array[i])
	}
	return
}

func MapI[T, U any](array []T, f func(i int) U) (result []U) {
	result = make([]U, len(array))
	for i := range array {
		result[i] = f(i)
	}
	return
}

func FirstNonNil[T any](first, second []T) []T {
	if first != nil {
		return first
	}
	return second
}

// Find the first element of the array matching the predicate. Returns -1 if no match is found
func Find[T any](array []T, f func(i int) bool) int {
	for i := range array {
		if f(i) {
			return i
		}
	}
	return -1
}

// Contains identifies if the reference value is contained within the array
func Contains[Comp comparable](arr []Comp, ref Comp) bool {
	for i := range arr {
		if arr[i] == ref {
			return true
		}
	}
	return false
}

// Initialise creates a new slice of the specified length, where each element is initialised as the
// provided default value.
func Initialise[T any](length int, defaultValue T) (result []T) {
	result = make([]T, length)
	for i := range result {
		result[i] = defaultValue
	}
	return result
}
