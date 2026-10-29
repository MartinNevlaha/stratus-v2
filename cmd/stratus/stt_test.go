package main

import (
	"slices"
	"testing"
)

func TestSTTRunArgs_GPU(t *testing.T) {
	args := sttRunArgs("Systran/faster-whisper-large-v3", true)

	if !slices.Contains(args, sttImageCUDA) {
		t.Errorf("args %v missing CUDA image", args)
	}
	i := slices.Index(args, "--gpus")
	if i < 0 || i+1 >= len(args) || args[i+1] != "all" {
		t.Errorf("args %v missing --gpus all", args)
	}
	if !slices.Contains(args, "WHISPER__MODEL=Systran/faster-whisper-large-v3") {
		t.Errorf("args %v missing model env", args)
	}
	if args[len(args)-1] != sttImageCUDA {
		t.Errorf("image must be the last argument, got %q", args[len(args)-1])
	}
}

func TestSTTRunArgs_CPU(t *testing.T) {
	args := sttRunArgs("m", false)

	if args[len(args)-1] != sttImageCPU {
		t.Errorf("image = %q, want %q", args[len(args)-1], sttImageCPU)
	}
	if slices.Contains(args, "--gpus") {
		t.Errorf("args %v must not request GPU", args)
	}
}
