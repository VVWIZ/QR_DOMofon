package onboarding

import (
	"fmt"
	"testing"

	"domofon/backend/internal/platform/httpx"
)

func TestNormalizeGrantPublicIDs_DedupAndTrim(t *testing.T) {
	in := []string{" a ", "a", "b", "", "  ", "b", "c"}
	out, herr := NormalizeGrantPublicIDs(in)
	if herr != nil {
		t.Fatalf("herr = %+v, want nil", herr)
	}
	want := []string{"a", "b", "c"}
	if fmt.Sprint(out) != fmt.Sprint(want) {
		t.Errorf("out = %v, want %v (дедуп + trim + порядок)", out, want)
	}
}

func TestNormalizeGrantPublicIDs_EmptyIsValid(t *testing.T) {
	out, herr := NormalizeGrantPublicIDs(nil)
	if herr != nil {
		t.Fatalf("herr = %+v, want nil", herr)
	}
	if len(out) != 0 {
		t.Errorf("out = %v, want пустой (нет доп. точек — валидно)", out)
	}
}

func TestNormalizeGrantPublicIDs_CapExceeded(t *testing.T) {
	in := make([]string, MaxCompositeGrants+1)
	for i := range in {
		in[i] = fmt.Sprintf("id-%02d", i) // все уникальны
	}
	_, herr := NormalizeGrantPublicIDs(in)
	if herr == nil {
		t.Fatalf("herr = nil, want VALIDATION_ERROR (превышен кэп)")
	}
	if herr.Code != httpx.CodeValidationError {
		t.Errorf("code = %q, want %q", herr.Code, httpx.CodeValidationError)
	}
}

func TestNormalizeGrantPublicIDs_CapAfterDedup(t *testing.T) {
	// MaxCompositeGrants+5 значений, но все дубли одного → после дедупа 1, кэп ок.
	in := make([]string, MaxCompositeGrants+5)
	for i := range in {
		in[i] = "same"
	}
	out, herr := NormalizeGrantPublicIDs(in)
	if herr != nil {
		t.Fatalf("herr = %+v, want nil (после дедупа в пределах кэпа)", herr)
	}
	if len(out) != 1 {
		t.Errorf("out = %v, want 1 элемент", out)
	}
}
