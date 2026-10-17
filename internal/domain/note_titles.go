package domain

import (
	"errors"
	"strings"
	"unicode/utf8"
)

const XHSTitleMaxCharacters = 20

// Count Unicode code points consistently with the editor, including emoji.
func ValidateNoteTitle(_, title string) error {
	if strings.TrimSpace(title) == "" {
		return errors.New("标题不能为空")
	}
	if utf8.RuneCountInString(title) > XHSTitleMaxCharacters {
		return errors.New("小红书标题不能超过 20 个字符，请缩短标题后保存并确认")
	}
	return nil
}
