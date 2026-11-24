package config

import (
	"reflect"
	"testing"

	"github.com/spf13/viper"
)

func TestConfigurationDefaultsTable(t *testing.T) {
	v := viper.New()
	setDefaults(v)
	for _, setting := range configurationDefaults {
		t.Run(setting.key, func(t *testing.T) {
			if got := v.Get(setting.key); !reflect.DeepEqual(got, setting.value) {
				t.Fatalf("default = %#v, want %#v", got, setting.value)
			}
		})
	}
}

func TestCollectionDefaultsAreIndependent(t *testing.T) {
	first, second := viper.New(), viper.New()
	setDefaults(first)
	setDefaults(second)
	first.GetStringSlice("scia.google_hosts")[0] = "changed.example"
	if got := second.GetStringSlice("scia.google_hosts"); !reflect.DeepEqual(got, []string{"www.googleapis.com"}) {
		t.Fatalf("collection default leaked between instances: %v", got)
	}
}
