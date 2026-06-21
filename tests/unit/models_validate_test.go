package unit_test

import (
	"testing"

	"mugi/internal/models"
)

func TestTaskValidate(t *testing.T) {
	if err := (&models.Task{Description: "   "}).Validate(); err == nil {
		t.Fatal("empty task description should fail validation")
	}
	if err := (&models.Task{Description: "build a CLI"}).Validate(); err != nil {
		t.Fatalf("valid task rejected: %v", err)
	}
}

func TestPlanValidate(t *testing.T) {
	if err := (&models.Plan{}).Validate(); err == nil {
		t.Fatal("plan with no steps should fail validation")
	}
	blank := &models.Plan{Steps: []models.Step{{ID: 1, Title: "   "}}}
	if err := blank.Validate(); err == nil {
		t.Fatal("step with blank title should fail validation")
	}
	ok := &models.Plan{Steps: []models.Step{{ID: 1, Title: "do the thing"}}}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid plan rejected: %v", err)
	}
}

func TestArtifactValidate(t *testing.T) {
	if err := (&models.Artifact{}).Validate(); err == nil {
		t.Fatal("artifact with no files should fail validation")
	}
	traversal := &models.Artifact{Files: []models.File{{Path: "../evil.go", Content: "x"}}}
	if err := traversal.Validate(); err == nil {
		t.Fatal("traversal path should fail validation")
	}
	empty := &models.Artifact{Files: []models.File{{Path: "main.go", Content: "  "}}}
	if err := empty.Validate(); err == nil {
		t.Fatal("empty file content should fail validation")
	}
	ok := &models.Artifact{Files: []models.File{{Path: "pkg/main.go", Content: "package main"}}}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid artifact rejected: %v", err)
	}
}

func TestReviewValidate(t *testing.T) {
	if err := (&models.Review{Score: 11}).Validate(); err == nil {
		t.Fatal("score above 10 should fail validation")
	}
	if err := (&models.Review{Score: -1}).Validate(); err == nil {
		t.Fatal("negative score should fail validation")
	}
	if err := (&models.Review{Score: 7}).Validate(); err != nil {
		t.Fatalf("in-range score rejected: %v", err)
	}
}
