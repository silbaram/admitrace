package kube136

import "testing"

func TestEquivalentSubresourceSupported(t *testing.T) {
	tests := []struct {
		name               string
		requestSubresource string
		targetSubresource  string
		want               bool
	}{
		{
			name: "main resource",
			want: true,
		},
		{
			name:               "same subresource",
			requestSubresource: "scale",
			targetSubresource:  "scale",
			want:               true,
		},
		{
			name:               "cross subresource",
			requestSubresource: "status",
			targetSubresource:  "scale",
		},
		{
			name:               "case sensitive",
			requestSubresource: "status",
			targetSubresource:  "Status",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := EquivalentSubresourceSupported(test.requestSubresource, test.targetSubresource)
			if got != test.want {
				t.Errorf("EquivalentSubresourceSupported(%q, %q) = %t, want %t", test.requestSubresource, test.targetSubresource, got, test.want)
			}
		})
	}
}
