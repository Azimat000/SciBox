package num

import (
	"math"
	"testing"
)

func TestInt16(t *testing.T) {
	for _, tc := range []struct {
		in   int
		want int16
	}{
		{0, 0}, {-5, -5}, {2026, 2026},
		{math.MaxInt16, math.MaxInt16}, {math.MaxInt16 + 1, math.MaxInt16}, {math.MaxInt64, math.MaxInt16},
		{math.MinInt16, math.MinInt16}, {math.MinInt16 - 1, math.MinInt16}, {math.MinInt64, math.MinInt16},
	} {
		if got := Int16(tc.in); got != tc.want {
			t.Errorf("Int16(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestInt32(t *testing.T) {
	for _, tc := range []struct {
		in   int
		want int32
	}{
		{0, 0}, {-5, -5}, {50, 50},
		{math.MaxInt32, math.MaxInt32}, {math.MaxInt32 + 1, math.MaxInt32}, {math.MaxInt64, math.MaxInt32},
		{math.MinInt32, math.MinInt32}, {math.MinInt32 - 1, math.MinInt32}, {math.MinInt64, math.MinInt32},
	} {
		if got := Int32(tc.in); got != tc.want {
			t.Errorf("Int32(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestUint32(t *testing.T) {
	for _, tc := range []struct {
		in   int
		want uint32
	}{
		{0, 0}, {32, 32}, {-1, 0}, {math.MinInt64, 0},
		{math.MaxUint32, math.MaxUint32}, {math.MaxUint32 + 1, math.MaxUint32}, {math.MaxInt64, math.MaxUint32},
	} {
		if got := Uint32(tc.in); got != tc.want {
			t.Errorf("Uint32(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestInt16Ptr(t *testing.T) {
	if Int16Ptr(nil) != nil {
		t.Fatal("nil must stay nil")
	}
	v := 1 << 20
	if got := Int16Ptr(&v); got == nil || *got != math.MaxInt16 {
		t.Fatalf("Int16Ptr(%d) = %v", v, got)
	}
}

func TestInt32s(t *testing.T) {
	got := Int32s([]int{1, math.MaxInt64, -3})
	want := []int32{1, math.MaxInt32, -3}
	if len(got) != len(want) {
		t.Fatalf("len = %d", len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] = %d, want %d", i, got[i], want[i])
		}
	}
	if Int32s(nil) == nil {
		t.Error("empty input must give an empty, non-nil slice (sqlc arrays)")
	}
}
