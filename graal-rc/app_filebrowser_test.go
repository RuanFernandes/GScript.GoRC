package main

import (
	"testing"

	"graal-rc/rclib"
)

func TestFileBrowserPathHelpersRejectFolderRenames(t *testing.T) {
	for _, path := range []string{"folder/", `folder\`, " folder/ "} {
		if !isFileBrowserDirectoryPath(path) {
			t.Fatalf("isFileBrowserDirectoryPath(%q) = false", path)
		}
	}
	for _, path := range []string{"file.txt", `folder\file.txt`, " folder.txt "} {
		if isFileBrowserDirectoryPath(path) {
			t.Fatalf("isFileBrowserDirectoryPath(%q) = true", path)
		}
	}

	entries := []rclib.FileBrowserEntry{
		{Path: "folder/", IsDirectory: true},
		{Path: `folder\file.txt`, IsDirectory: false},
	}
	if !fileBrowserEntryIsDirectory(entries, `folder\`) {
		t.Fatal("directory entry with slash normalization was not detected")
	}
	if fileBrowserEntryIsDirectory(entries, "folder/file.txt") {
		t.Fatal("regular file was detected as a directory")
	}
}

func TestCodingSettingsAcceptOnlySupportedTabSizes(t *testing.T) {
	if DefaultCodingSettings.TabSize != 2 {
		t.Fatalf("default tab size = %d, want 2", DefaultCodingSettings.TabSize)
	}
	for _, size := range []int{1, 2, 4} {
		if !isSupportedTabSize(size) {
			t.Fatalf("tab size %d was rejected", size)
		}
	}
	for _, size := range []int{0, 3, 5, -1} {
		if isSupportedTabSize(size) {
			t.Fatalf("tab size %d was accepted", size)
		}
	}
}

func TestExternalEditorSettingsAcceptOnlySupportedPresets(t *testing.T) {
	if DefaultCodingSettings.ExternalEditor != externalEditorVSCode {
		t.Fatalf("default external editor = %q, want %q", DefaultCodingSettings.ExternalEditor, externalEditorVSCode)
	}
	for _, editor := range []string{externalEditorVSCode, externalEditorSublime, externalEditorNotepadPlus} {
		if !isSupportedExternalEditor(editor) {
			t.Fatalf("external editor %q was rejected", editor)
		}
		if normalizeExternalEditor("  "+editor+"  ") != editor {
			t.Fatalf("external editor %q was not normalized", editor)
		}
	}
	for _, editor := range []string{"", "vim", "notepad", "vscode --wait"} {
		if isSupportedExternalEditor(editor) {
			t.Fatalf("unsupported external editor %q was accepted", editor)
		}
		if normalizeExternalEditor(editor) != externalEditorVSCode {
			t.Fatalf("invalid external editor %q did not fall back to %q", editor, externalEditorVSCode)
		}
	}
}

func TestChatSettingsNormalizeColorsAndPaths(t *testing.T) {
	settings := normalizeChatSettings(ChatSettings{
		Timestamp: " #001eff ",
		RCPrefix:  "invalid",
		NCPrefix:  "#38bdf8",
		LogDir:    "  C:/chat  ",
		PMLogDir:  "  C:/pm  ",
	})
	if settings.Timestamp != "#001EFF" {
		t.Fatalf("timestamp color = %q, want uppercase normalized hex", settings.Timestamp)
	}
	if settings.RCPrefix != DefaultChatSettings.RCPrefix {
		t.Fatalf("invalid RC prefix color = %q, want default %q", settings.RCPrefix, DefaultChatSettings.RCPrefix)
	}
	if settings.LogDir != "C:/chat" || settings.PMLogDir != "C:/pm" {
		t.Fatalf("log directories were not trimmed: %q / %q", settings.LogDir, settings.PMLogDir)
	}
}

func TestImageThumbnailMIMEAcceptsOnlySupportedImageExtensions(t *testing.T) {
	for _, path := range []string{"preview.png", "folder/photo.JPG", "icon.webp", "favicon.ico"} {
		if mimeType, ok := imageThumbnailMIME(path); !ok || mimeType == "" {
			t.Fatalf("imageThumbnailMIME(%q) = %q, %v; want a supported MIME type", path, mimeType, ok)
		}
	}
	for _, path := range []string{"notes.txt", "movie.mp4", "archive.zip", "folder/"} {
		if mimeType, ok := imageThumbnailMIME(path); ok || mimeType != "" {
			t.Fatalf("imageThumbnailMIME(%q) = %q, %v; want unsupported", path, mimeType, ok)
		}
	}
}

func TestFileBrowserImageThumbnailsAreDisabledByDefault(t *testing.T) {
	if (FileBrowserConfig{}).ShowImageThumbnails {
		t.Fatal("image thumbnails must be disabled by default")
	}
	if maxImageThumbnailBytes != 500*1024 {
		t.Fatalf("thumbnail limit = %d bytes, want %d", maxImageThumbnailBytes, 500*1024)
	}
}
