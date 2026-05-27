package main

import (
	"reflect"
	"testing"
)

func TestCodexNewArgs(t *testing.T) {
	got := codexNewArgs("/tmp/work")
	want := []string{"-C", "/tmp/work"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestCodexResumeArgs(t *testing.T) {
	got := codexResumeArgs("019d-meta", "/tmp/work")
	want := []string{"resume", "019d-meta", "-C", "/tmp/work"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
