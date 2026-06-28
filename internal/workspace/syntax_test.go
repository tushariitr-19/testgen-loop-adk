package workspace

import (
	"strings"
	"testing"
)

func TestValidateTestFunc_Valid(t *testing.T) {
	src := `func TestThing(t *testing.T) {
	if 1 != 1 {
		t.Fatal("nope")
	}
}`
	name, err := ValidateTestFunc(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "TestThing" {
		t.Errorf("name = %q, want TestThing", name)
	}
}

func TestValidateTestFunc_RejectsNonTestPrefix(t *testing.T) {
	src := `func helperThing(t *testing.T) {}`
	_, err := ValidateTestFunc(src)
	if err == nil || !strings.Contains(err.Error(), "must start with Test") {
		t.Errorf("expected Test-prefix error, got %v", err)
	}
}

func TestValidateTestFunc_RejectsWrongParamCount(t *testing.T) {
	src := `func TestThing() {}`
	_, err := ValidateTestFunc(src)
	if err == nil || !strings.Contains(err.Error(), "exactly one parameter") {
		t.Errorf("expected param-count error, got %v", err)
	}
}

func TestValidateTestFunc_RejectsWrongParamType(t *testing.T) {
	src := `func TestThing(n int) {}`
	_, err := ValidateTestFunc(src)
	if err == nil || !strings.Contains(err.Error(), "*testing.T") {
		t.Errorf("expected *testing.T error, got %v", err)
	}
}

func TestValidateTestFunc_RejectsReturnValue(t *testing.T) {
	src := `func TestThing(t *testing.T) error { return nil }`
	_, err := ValidateTestFunc(src)
	if err == nil || !strings.Contains(err.Error(), "must not return") {
		t.Errorf("expected return-value error, got %v", err)
	}
}

func TestValidateTestFunc_RejectsMethod(t *testing.T) {
	src := `func (s *S) TestThing(t *testing.T) {}`
	_, err := ValidateTestFunc(src)
	if err == nil || !strings.Contains(err.Error(), "method") {
		t.Errorf("expected method error, got %v", err)
	}
}

func TestValidateTestFunc_RejectsNonFunction(t *testing.T) {
	src := `var TestThing = 42`
	_, err := ValidateTestFunc(src)
	if err == nil {
		t.Error("expected error for non-function declaration")
	}
}

func TestValidateTestFunc_RejectsMultipleDecls(t *testing.T) {
	src := `func TestA(t *testing.T) {}
func TestB(t *testing.T) {}`
	_, err := ValidateTestFunc(src)
	if err == nil || !strings.Contains(err.Error(), "exactly one declaration") {
		t.Errorf("expected single-decl error, got %v", err)
	}
}

func TestValidateTestFunc_RejectsUnparseable(t *testing.T) {
	src := `func TestThing( {`
	_, err := ValidateTestFunc(src)
	if err == nil {
		t.Error("expected error for unparseable source")
	}
}

func TestValidateTestFunc_RejectsEmpty(t *testing.T) {
	_, err := ValidateTestFunc("   ")
	if err == nil {
		t.Error("expected error for empty source")
	}
}

func TestExtractTestNames_OnlyTestPrefix(t *testing.T) {
	src := `package x

import "testing"

func helper() {}
func TestA(t *testing.T) {}
func TestB(t *testing.T) {}
func TesUnrelated() {}
`
	names, err := ExtractTestNames(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("got %d names, want 2: %v", len(names), names)
	}
	if names[0] != "TestA" || names[1] != "TestB" {
		t.Errorf("names = %v, want [TestA TestB]", names)
	}
}

func TestExtractTestNames_NoTests(t *testing.T) {
	src := `package x
func helper() {}
`
	names, err := ExtractTestNames(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("got %d names, want 0", len(names))
	}
}

func TestExtractTestNames_Unparseable(t *testing.T) {
	_, err := ExtractTestNames(`package x; func (`)
	if err == nil {
		t.Error("expected parse error")
	}
}
