// Package num переводит числа между целыми типами без переполнения.
//
// Обычное приведение int32(v) при v больше 2 147 483 647 даёт отрицательное число:
// например, адрес с offset=2147483648 превращался в «OFFSET -2147483648» и ошибку базы.
// Здесь значение вне диапазона прижимается к ближайшей границе типа.
package num

import "math"

// Int16 возвращает v, прижатое к диапазону int16.
func Int16(v int) int16 {
	switch {
	case v > math.MaxInt16:
		return math.MaxInt16
	case v < math.MinInt16:
		return math.MinInt16
	}
	return int16(v)
}

// Int32 возвращает v, прижатое к диапазону int32.
func Int32(v int) int32 {
	switch {
	case v > math.MaxInt32:
		return math.MaxInt32
	case v < math.MinInt32:
		return math.MinInt32
	}
	return int32(v)
}

// Uint32 возвращает v, прижатое к диапазону uint32.
func Uint32(v int) uint32 {
	switch {
	case v > math.MaxUint32:
		return math.MaxUint32
	case v < 0:
		return 0
	}
	return uint32(v)
}

// Int16Ptr — Int16 для необязательного значения: nil остаётся nil.
func Int16Ptr(v *int) *int16 {
	if v == nil {
		return nil
	}
	return new(Int16(*v))
}

// Int32s переводит срез, прижимая каждое значение.
func Int32s(in []int) []int32 {
	out := make([]int32, len(in))
	for i, v := range in {
		out[i] = Int32(v)
	}
	return out
}
