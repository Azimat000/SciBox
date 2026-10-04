package issn

import "testing"

func TestNormalize(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"0028-0836", "0028-0836", true},   // Nature
		{"00280836", "0028-0836", true},    // так в файле SCImago
		{" 0028 0836 ", "0028-0836", true}, // пробелы
		{"ISSN 0028-0836", "0028-0836", true},
		{"issn: 0028-0836", "0028-0836", true},
		{"2041-1723", "2041-1723", true}, // Nature Communications
		{"0031-899X", "0031-899X", true}, // контрольная «X»
		{"0031-899x", "0031-899X", true}, // строчная «x»
		{"0031899X", "0031-899X", true},
		{"0028–0836", "0028-0836", true}, // типографское тире
		{"0028‐0836", "0028-0836", true}, // дефис U+2010
		{"0028-0837", "", false},         // не та контрольная цифра
		{"0031-8990", "", false},         // нужна X
		{"0031-89X9", "", false},         // X не на последнем месте
		{"002-80836", "0028-0836", true}, // дефис не там, цифры те же
		{"0028-083", "", false},          // коротко
		{"0028-08366", "", false},        // длинно
		{"abcd-efgh", "", false},
		{"", "", false},
		{"ISSN", "", false},
		{"10.1038/nature", "", false}, // DOI
	}
	for _, c := range cases {
		got, ok := Normalize(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("Normalize(%q) = %q, %v; want %q, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}
