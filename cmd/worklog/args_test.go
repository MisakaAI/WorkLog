package main

import (
	"errors"
	"flag"
	"reflect"
	"testing"
)

func TestParseArgs(t *testing.T) {
	for _, tt := range []struct {
		args []string
		conf string
		rest []string
	}{
		{nil, "", nil},
		{[]string{"--conf=/etc/worklog"}, "/etc/worklog", []string{}},
		{[]string{"--conf", "/etc/worklog/config.json", "stdio"}, "/etc/worklog/config.json", []string{"stdio"}},
		{[]string{"--conf=/etc/worklog", "backup", "backup.db"}, "/etc/worklog", []string{"backup", "backup.db"}},
		{[]string{"restore", "backup.db"}, "", []string{"restore", "backup.db"}},
	} {
		conf, rest, err := parseArgs(tt.args)
		if err != nil || conf != tt.conf || !reflect.DeepEqual(rest, tt.rest) {
			t.Fatalf("parseArgs(%v) = %q %v %v", tt.args, conf, rest, err)
		}
	}
	for _, args := range [][]string{{"--conf="}, {"--conf"}, {"--unknown"}} {
		if _, _, err := parseArgs(args); err == nil {
			t.Fatalf("accepted invalid options: %v", args)
		}
	}
	if _, _, err := parseArgs([]string{"--help"}); !errors.Is(err, flag.ErrHelp) {
		t.Fatal("help not handled", err)
	}
}
