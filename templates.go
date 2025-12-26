package main

import (
	_ "embed"
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

type templateData struct {
	Style template.CSS
	Data  any
}

//go:embed templates/list.gohtml
var listTemplateString string

func initListTemplate() (err error) {
	listTemplate, err = template.New("List").Parse(strings.TrimSpace(listTemplateString))
	return
}

//go:embed templates/urlform.gohtml
var urlFormTemplateString string

func initURLFormTemplate() (err error) {
	urlFormTemplate, err = template.New("UrlForm").Parse(strings.TrimSpace(urlFormTemplateString))
	return
}

//go:embed templates/textform.gohtml
var textFormTemplateString string

func initTextFormTemplate() (err error) {
	textFormTemplate, err = template.New("TextForm").Parse(strings.TrimSpace(textFormTemplateString))
	return
}

//go:embed static/style.css
var styleCSS string
