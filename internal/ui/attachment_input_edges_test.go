package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rmarchei/keetui/internal/config"
)

func TestAttachmentPathInputNormalizesPastedFilePaths(t *testing.T) {
	for _, extracting := range []bool{false, true} {
		for _, path := range []struct{ raw, want string }{
			{`  "/synthetic/path with spaces.txt"  `, "/synthetic/path with spaces.txt"},
			{`'relative path/file.txt'`, "relative path/file.txt"},
			{`folder\ with\ spaces/file.txt`, "folder with spaces/file.txt"},
			{"plain-file.txt", "plain-file.txt"},
		} {
			m := NewDetailModel(testEntry(), config.Default())
			m.attachMode, m.extractingAttachment, m.addingAttachment = true, extracting, !extracting
			m.extractAttachName = "synthetic-attachment.txt"
			m.attachPathInput.SetValue(path.raw)
			updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			if cmd == nil {
				t.Fatalf("path %q did not produce an attachment request", path.raw)
			}
			var got string
			switch msg := cmd().(type) {
			case AddAttachmentMsg:
				if extracting {
					t.Fatal("extract path emitted an add request")
				}
				got = msg.FilePath
			case ExtractAttachmentMsg:
				if !extracting || msg.Name != m.extractAttachName {
					t.Fatal("wrong extraction request")
				}
				got = msg.DestPath
			default:
				t.Fatalf("unexpected path action %T", msg)
			}
			if got != path.want {
				t.Fatalf("path %q became %q, want %q", path.raw, got, path.want)
			}
			next := updated.(DetailModel)
			if next.addingAttachment || next.extractingAttachment || next.attachPathInput.Value() != "" {
				t.Fatal("confirmed attachment path was not cleared")
			}
		}
	}
}

func TestAttachmentPathInputEmptyConfirmationKeepsTheFormOpen(t *testing.T) {
	for _, extracting := range []bool{false, true} {
		for _, value := range []string{"", "   ", `""`, "''"} {
			m := NewDetailModel(testEntry(), config.Default())
			m.attachMode, m.extractingAttachment, m.addingAttachment = true, extracting, !extracting
			m.attachPathInput.SetValue(value)
			updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			next := updated.(DetailModel)
			if cmd != nil || next.extractingAttachment != extracting || next.addingAttachment == extracting {
				t.Fatalf("empty path %q was accepted or dismissed the form", value)
			}
		}
	}
}
