package e2e

import (
	"math/rand"
	"strings"
)

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

// https://stackoverflow.com/questions/22892120/how-to-generate-a-random-string-of-a-fixed-length-in-go/22892986#22892986
func randomString(length int) string {
	b := make([]rune, length)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func resourceBuilder(resource string, name string, values map[string]string, identifiers map[string]string, config map[string]string) (string, error) {
	sb := strings.Builder{}
	_, err := sb.WriteString("resource \"" + resource + "\" \"" + name + "\" {\n")
	if err != nil {
		return "", err
	}

	for k, v := range identifiers {
		if v == "" {
			continue
		}
		_, err = sb.WriteString(k + " = " + v + "\n")
		if err != nil {
			return "", err
		}
	}

	for k, v := range values {
		if v == "" {
			continue
		}
		_, err = sb.WriteString(k + " = \"" + v + "\"\n")
		if err != nil {
			return "", err
		}
	}
	for k, v := range config {
		if v == "" {
			continue
		}
		_, err = sb.WriteString(k + " = " + "<<EOF\n" + v + "\nEOF\n")
		if err != nil {
			return "", err
		}
	}

	_, err = sb.WriteString("}\n")
	if err != nil {
		return "", err
	}

	return sb.String(), nil
}
