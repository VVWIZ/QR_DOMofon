package onboarding

import "strings"

// NormalizePhone приводит телефон к каноничному виду (+<цифры>): выбрасывает
// разделители (пробелы, скобки, дефисы), локальную запись через ведущую 8
// (11 цифр) переводит в +7, гарантирует ведущий «+».
//
// Без нормализации «+77010000010» и «8 (701) 000-00-10» дали бы РАЗНЫХ
// пользователей (users.phone UNIQUE по строке): дубли аккаунтов и инвайт «не
// тому». Пустая строка на выходе → телефон невалиден (вызывающий отдаёт
// VALIDATION_ERROR).
func NormalizePhone(raw string) string {
	var digits strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	d := digits.String()
	if d == "" {
		return ""
	}
	if len(d) == 11 && d[0] == '8' {
		d = "7" + d[1:]
	}
	return "+" + d
}
