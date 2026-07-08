package main

import (
	"fmt"
	"testing"
	"time"
)

var idemBase = time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)

func TestIdempotency_FirstSeenIsFalse(t *testing.T) {
	b := NewIdempotencyBuffer()
	if b.Seen("req-1", idemBase) {
		t.Fatal("первый вызов с новым request_id должен вернуть false")
	}
}

func TestIdempotency_DuplicateWithinTTL(t *testing.T) {
	b := NewIdempotencyBuffer()
	b.Seen("req-1", idemBase)
	if !b.Seen("req-1", idemBase.Add(30*time.Second)) {
		t.Fatal("повтор в пределах 60с должен вернуть true (дубликат)")
	}
}

func TestIdempotency_ExpiresAfterTTL(t *testing.T) {
	b := NewIdempotencyBuffer()
	b.Seen("req-1", idemBase)
	if b.Seen("req-1", idemBase.Add(61*time.Second)) {
		t.Fatal("после 60с TTL запись должна истечь → снова false")
	}
}

func TestIdempotency_EvictsOldestOnOverflow(t *testing.T) {
	b := NewIdempotencyBuffer()
	// Заполняем буфер 20 уникальными id — каждый впервые, значит false.
	for i := 1; i <= idempotencyCapacity; i++ {
		id := fmt.Sprintf("req-%02d", i)
		if b.Seen(id, idemBase) {
			t.Fatalf("id %s встречается впервые → ожидался false", id)
		}
	}
	// 21-й уникальный id переполняет буфер и вытесняет самый старый (req-01).
	if b.Seen("req-21", idemBase) {
		t.Fatal("21-й уникальный id → false")
	}
	// req-02 всё ещё в буфере → дубликат (проверяем ДО повторной вставки req-01).
	if !b.Seen("req-02", idemBase) {
		t.Fatal("req-02 ещё в буфере → true")
	}
	// req-01 вытеснен переполнением → снова считается новым.
	if b.Seen("req-01", idemBase) {
		t.Fatal("req-01 вытеснен переполнением → должен вернуть false")
	}
}
