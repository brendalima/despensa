package main

import (
	"slices"
	"testing"
)

// The flag package stops at the first positional argument, which silently
// dropped every --portion in "add arroz g --portion taça=180".
func TestSplitArgs(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		wantPositional []string
		wantFlags      []string
	}{
		{
			name:           "flags after the positionals",
			args:           []string{"arroz", "g", "--portion", "taça=180", "--portion", "pacote=5000"},
			wantPositional: []string{"arroz", "g"},
			wantFlags:      []string{"--portion", "taça=180", "--portion", "pacote=5000"},
		},
		{
			name:           "flags before the positionals",
			args:           []string{"--portion", "fatia=1", "pão de forma", "un"},
			wantPositional: []string{"pão de forma", "un"},
			wantFlags:      []string{"--portion", "fatia=1"},
		},
		{
			name:           "a flag written with an equals sign takes no lookahead",
			args:           []string{"arroz", "--threshold=360", "g"},
			wantPositional: []string{"arroz", "g"},
			wantFlags:      []string{"--threshold=360"},
		},
		{
			name:           "a trailing flag with no value",
			args:           []string{"arroz", "--portion"},
			wantPositional: []string{"arroz"},
			wantFlags:      []string{"--portion"},
		},
		{
			name:           "no flags at all",
			args:           []string{"arroz", "taça", "2"},
			wantPositional: []string{"arroz", "taça", "2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			positional, flags := splitArgs(tt.args)
			if !slices.Equal(positional, tt.wantPositional) {
				t.Errorf("positional = %q, want %q", positional, tt.wantPositional)
			}
			if !slices.Equal(flags, tt.wantFlags) {
				t.Errorf("flags = %q, want %q", flags, tt.wantFlags)
			}
		})
	}
}
