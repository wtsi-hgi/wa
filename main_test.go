package main

import (
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

func TestRewriteLegacyInspectArgs(t *testing.T) {
	convey.Convey("single bare identifiers are rewritten to saga inspect", t, func() {
		convey.So(rewriteLegacyInspectArgs([]string{"6568"}), convey.ShouldResemble, []string{"saga", "inspect", "6568"})
		convey.So(rewriteLegacyInspectArgs([]string{"AM762808"}), convey.ShouldResemble, []string{"saga", "inspect", "AM762808"})
		convey.So(rewriteLegacyInspectArgs([]string{"--token", "test", "6568"}), convey.ShouldResemble, []string{"saga", "inspect", "--token", "test", "6568"})
		convey.So(rewriteLegacyInspectArgs([]string{"6568", "--token", "test"}), convey.ShouldResemble, []string{"saga", "inspect", "6568", "--token", "test"})
	})

	convey.Convey("explicit subcommands and flags are left unchanged", t, func() {
		convey.So(rewriteLegacyInspectArgs([]string{"results", "search"}), convey.ShouldResemble, []string{"results", "search"})
		convey.So(rewriteLegacyInspectArgs([]string{"--help"}), convey.ShouldResemble, []string{"--help"})
		convey.So(rewriteLegacyInspectArgs([]string{"saga"}), convey.ShouldResemble, []string{"saga"})
		convey.So(rewriteLegacyInspectArgs([]string{"delete"}), convey.ShouldResemble, []string{"delete"})
	})
}
