package util

import "testing"

func TestTarFilename(t *testing.T) {
	t.Parallel()
	got := TarFilename("ghcr.io/imythu/rflush:latest-beta")
	want := "ghcr.io_imythu_rflush_latest-beta.tar"
	if got != want {
		t.Fatalf("TarFilename() = %q, want %q", got, want)
	}
}
