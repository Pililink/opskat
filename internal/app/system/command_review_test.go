package system

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCommandReviewSaveInputValidate(t *testing.T) {
	valid := []CommandReviewSaveInput{
		{},
		{Model: "jev-1.13.0", TimeoutMs: 1000, Threshold: 0.3},
		{TimeoutMs: 60000, Threshold: 0.99},
	}
	for _, in := range valid {
		assert.NoError(t, in.validate(), "%+v", in)
	}

	invalid := []CommandReviewSaveInput{
		{Model: "jev-latest"},
		{Model: " jev-preview "},
		{TimeoutMs: 999},
		{TimeoutMs: 60001},
		{Threshold: -0.1},
		{Threshold: 1},
	}
	for _, in := range invalid {
		assert.Error(t, in.validate(), "%+v", in)
	}
}
