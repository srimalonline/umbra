package safety

import (
	"strings"
	"testing"
)

func TestSanitizeName(t *testing.T) {
	if got, err := SanitizeName("My App!!"); err != nil || got != "my-app" {
		t.Fatalf("SanitizeName(My App!!) = %q, %v; want my-app", got, err)
	}
	if got, err := SanitizeName("  a__b  "); err != nil || got != "a-b" {
		t.Fatalf("SanitizeName(  a__b  ) = %q, %v; want a-b", got, err)
	}
	if _, err := SanitizeName("!!!"); err == nil {
		t.Fatalf("SanitizeName(!!!) should error")
	}
}

func TestValidateDomain(t *testing.T) {
	if got, err := ValidateDomain("app.example.com"); err != nil || got != "app.example.com" {
		t.Fatalf("ValidateDomain(app.example.com) = %q, %v", got, err)
	}
	if _, err := ValidateDomain("not a domain"); err == nil {
		t.Fatalf("ValidateDomain(not a domain) should error")
	}
	if _, err := ValidateDomain("evil.com { }"); err == nil {
		t.Fatalf("ValidateDomain(evil.com { }) should error (directive injection)")
	}
}

func TestCapOutput(t *testing.T) {
	if len(CapOutput(strings.Repeat("x", 20000))) >= 20000 {
		t.Fatalf("CapOutput did not cap a 20k string")
	}
	if CapOutput("short") != "short" {
		t.Fatalf("CapOutput mangled a short string")
	}
}

func TestValidatePort(t *testing.T) {
	if _, err := ValidatePort(0); err == nil {
		t.Fatalf("port 0 should be invalid")
	}
	if _, err := ValidatePort(70000); err == nil {
		t.Fatalf("port 70000 should be invalid")
	}
	if p, err := ValidatePort(8080); err != nil || p != 8080 {
		t.Fatalf("port 8080 should be valid")
	}
}

func TestClampLines(t *testing.T) {
	if ClampLines(0) != 200 {
		t.Fatalf("unset lines should default to 200")
	}
	if ClampLines(5000) != 1000 {
		t.Fatalf("lines should clamp to 1000")
	}
	if ClampLines(50) != 50 {
		t.Fatalf("in-range lines should pass through")
	}
}
