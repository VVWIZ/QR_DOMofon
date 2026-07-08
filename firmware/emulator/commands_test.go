package main

import (
	"testing"
	"time"
)

func TestIsStale(t *testing.T) {
	base := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	// Проверяем только значения ЗА пределами допуска ±5с (вне интервала 25–35с);
	// саму границу 30с не проверяем — она размыта допуском на рассинхрон часов.
	cases := []struct {
		name     string
		issuedAt time.Time
		now      time.Time
		want     bool
	}{
		{"10с в прошлом — свежая", base, base.Add(10 * time.Second), false},
		{"20с в прошлом — свежая (<25с)", base, base.Add(20 * time.Second), false},
		{"40с в прошлом — устарела (>35с)", base, base.Add(40 * time.Second), true},
		{"45с в прошлом — устарела", base, base.Add(45 * time.Second), true},
		{"3с в будущем — свежая", base.Add(3 * time.Second), base, false},
		{"40с в будущем — устарела (>35с)", base.Add(40 * time.Second), base, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsStale(tc.issuedAt, tc.now); got != tc.want {
				t.Fatalf("IsStale(issuedAt, now) = %v, want %v", got, tc.want)
			}
		})
	}
}
