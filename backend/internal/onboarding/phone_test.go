package onboarding

import "testing"

func TestNormalizePhone(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"канонический не меняется", "+77010000010", "+77010000010"},
		{"разделители выбрасываются", "+7 (701) 000-00-10", "+77010000010"},
		{"без плюса — плюс добавляется", "77010000010", "+77010000010"},
		{"локальная 8 → +7", "87010000010", "+77010000010"},
		{"локальная 8 с разделителями", "8 (701) 000-00-10", "+77010000010"},
		{"пусто → пусто (невалиден)", "", ""},
		{"только мусор → пусто", "+()- ", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NormalizePhone(c.in); got != c.want {
				t.Errorf("NormalizePhone(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// Разные записи одного номера должны схлопываться в один ключ — иначе дубли
// пользователей (users.phone UNIQUE по строке).
func TestNormalizePhone_SameNumberSameKey(t *testing.T) {
	variants := []string{"+77010000010", "77010000010", "87010000010", "8 701 000 00 10"}
	first := NormalizePhone(variants[0])
	for _, v := range variants[1:] {
		if got := NormalizePhone(v); got != first {
			t.Errorf("NormalizePhone(%q) = %q, want %q (тот же номер)", v, got, first)
		}
	}
}
