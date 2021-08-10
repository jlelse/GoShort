package main

import (
	"html/template"
	"log"
	"strings"
)

var listTemplate *template.Template
var urlFormTemplate *template.Template
var textFormTemplate *template.Template

func init() {
	if initListTemplate() != nil || initURLFormTemplate() != nil || initTextFormTemplate() != nil {
		log.Fatal("Failed to initialize templates")
		return
	}
}

const listTemplateString = `
<!doctype html>
<html lang=en>
<meta name=viewport content="width=device-width, initial-scale=1.0">
<title>Short URLs</title>
<h1>Short URLs</h1>
<table>
<tr><th>slug</th><th>url</th><th>hits</th></tr>
{{range .}}<tr><td>{{.Slug}}</td><td>{{.URL}}</td><td>{{.Hits}}</td></tr>{{end}}
</table>
</html>
`

func initListTemplate() (err error) {
	listTemplate, err = template.New("List").Parse(strings.TrimSpace(listTemplateString))
	return
}

const urlFormTemplateString = `
<!doctype html>
<html lang=en>
<meta name=viewport content="width=device-width, initial-scale=1.0">
<title>{{.Title}}</title>
<h1>{{.Title}}</h1>
<form action={{.URL}} method=post>
{{range .Fields}}<input type=text name={{index . 0}} placeholder={{index . 0}} value="{{index . 1}}"><br><br>{{end}}
<input type=submit value={{.Title}}>
</form>
</html>
`

func initURLFormTemplate() (err error) {
	urlFormTemplate, err = template.New("UrlForm").Parse(strings.TrimSpace(urlFormTemplateString))
	return
}

const textFormTemplateString = `
<!doctype html>
<html lang=en>
<meta name=viewport content="width=device-width, initial-scale=1.0">
<title>{{.Title}}</title>
<h1>{{.Title}}</h1>
<form action={{.URL}} method=post>
{{range .Fields}}<input type=text name={{index . 0}} placeholder={{index . 0}} value="{{index . 1}}"><br><br>{{end}}
{{range .TextAreas}}<textarea name={{index . 0}} placeholder={{index . 0}}>{{index . 1}}</textarea><br><br>{{end}}
<input type=submit value={{.Title}}>
</form>
</html>
`

func initTextFormTemplate() (err error) {
	textFormTemplate, err = template.New("TextForm").Parse(strings.TrimSpace(textFormTemplateString))
	return
}
