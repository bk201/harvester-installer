package config

import (
	"reflect"
	"testing"
)

func TestReadConfig(t *testing.T) {
	tests := []struct {
		name    string
		want    HarvesterConfig
		wantErr bool
	}{
		{
			name: "test",
			want: HarvesterConfig{
				ServerURL: "myurl",
				Token:     "token",
				OS: OS{
					Hostname: "abc",
					Install: &Install{
						Device: "/dev/sda",
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReadConfig()
			if (err != nil) != tt.wantErr {
				t.Errorf("ReadConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ReadConfig() = %v, want %v", got, tt.want)
			}
		})
	}
}
