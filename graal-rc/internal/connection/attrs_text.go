package connection

import (
	"encoding/json"
	"strings"
)

// attrsEditorText is the editor document of a player-attributes reply. grclib
// decodes the animation as "gani" but writes the Animation line from another
// key, so the line is filled here.
func attrsEditorText(propertiesJSON, editorText string) string {
	var properties struct {
		Animation string `json:"gani"`
	}
	if json.Unmarshal([]byte(propertiesJSON), &properties) != nil {
		return editorText
	}
	// A line break would add lines to the document.
	if properties.Animation == "" || strings.ContainsAny(properties.Animation, "\r\n") {
		return editorText
	}
	// The first empty line is the one of [Look]; later sections are free text.
	return strings.Replace(editorText, "\nAnimation: \n", "\nAnimation: "+properties.Animation+"\n", 1)
}
