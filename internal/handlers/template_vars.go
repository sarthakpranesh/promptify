package handlers

import (
	"html"
	htemplate "html/template"
	"regexp"
	"strings"
)

var templateVarPattern = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)

func extractTemplateVariables(promptTemplate string) []string {
	matches := templateVarPattern.FindAllStringSubmatch(promptTemplate, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(matches))
	var variables []string
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		variable := strings.TrimSpace(match[1])
		if variable == "" {
			continue
		}
		if _, exists := seen[variable]; exists {
			continue
		}
		seen[variable] = struct{}{}
		variables = append(variables, variable)
	}

	return variables
}

func highlightTemplateVariables(promptTemplate string) htemplate.HTML {
	indices := templateVarPattern.FindAllStringIndex(promptTemplate, -1)
	if len(indices) == 0 {
		return htemplate.HTML(html.EscapeString(promptTemplate))
	}

	var builder strings.Builder
	last := 0
	for _, index := range indices {
		if len(index) != 2 {
			continue
		}
		start, end := index[0], index[1]
		if start < last || start > len(promptTemplate) || end > len(promptTemplate) {
			continue
		}

		builder.WriteString(html.EscapeString(promptTemplate[last:start]))
		builder.WriteString(`<span class="rounded bg-sky-100 px-1 text-sky-700">`)
		builder.WriteString(html.EscapeString(promptTemplate[start:end]))
		builder.WriteString(`</span>`)
		last = end
	}

	if last < len(promptTemplate) {
		builder.WriteString(html.EscapeString(promptTemplate[last:]))
	}

	return htemplate.HTML(builder.String())
}
