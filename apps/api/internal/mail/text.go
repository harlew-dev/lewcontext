package mail

import (
	"bytes"
	"fmt"
	"io/fs"
	"text/template"
)

// textTemplate renders an email's plain-text part: templates/<name>.txt's
// "content" inside templates/layout.txt, which opens with the app's name.
// This is the only file that may import text/template, which escapes nothing:
// right for plain text, where there is no markup to inject, and wrong for
// HTML (TestNoTextTemplate checks both). Names in the data can't add lines:
// the API refuses control characters in them.
type textTemplate struct{ t *template.Template }

func parseText(files fs.FS, name Template, app string) (textTemplate, error) {
	file := "templates/" + string(name) + ".txt"
	funcs := template.FuncMap{"app": func() string { return app }}
	t, err := template.New("layout.txt").Funcs(funcs).ParseFS(files, "templates/layout.txt", file)
	if err != nil {
		return textTemplate{}, fmt.Errorf("mail template %s is missing or broken: %w", file, err)
	}
	return textTemplate{t.Option("missingkey=error")}, nil
}

func (tt textTemplate) render(data any) (string, error) {
	var body bytes.Buffer
	if err := tt.t.ExecuteTemplate(&body, "layout.txt", data); err != nil {
		return "", err
	}
	return body.String(), nil
}
