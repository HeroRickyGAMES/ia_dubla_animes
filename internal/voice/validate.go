// Validação da voz detectada contra o roteiro ("bandaid").
package voice

import "fmt"

// RoleMap associa nome do personagem → papel esperado no roteiro.
type RoleMap map[string]Role

// ParseRoles converte "naruto:menino, sakura:menina, ..." em RoleMap.
func ParseRoles(s string) RoleMap {
	m := RoleMap{}
	for _, part := range splitComma(s) {
		if part == "" {
			continue
		}
		name, role := splitColon(part)
		if name == "" || role == "" {
			continue
		}
		m[name] = NormalizeRole(role)
	}
	return m
}

func splitComma(s string) []string {
	var out []string
	var cur []rune
	depth := 0
	for _, r := range s {
		switch r {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, string(cur))
				cur = nil
				continue
			}
		}
		cur = append(cur, r)
	}
	out = append(out, string(cur))
	return out
}

func splitColon(s string) (string, string) {
	for i, r := range s {
		if r == ':' {
			return trimSpace(s[:i]), trimSpace(s[i+1:])
		}
	}
	return trimSpace(s), ""
}

func trimSpace(s string) string {
	i, j := 0, len(s)-1
	for i <= j && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n') {
		i++
	}
	for j >= i && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n') {
		j--
	}
	if i > j {
		return ""
	}
	return s[i : j+1]
}

// CheckConflict compara o papel detectado pela voz com o esperado no roteiro.
// Retorna conflito + mensagem. Um "boy" com voz feminina (seiyuu feminina
// dubla meninos) é conflito CLÁSSICO — sinalizamos e sugerimos tratamento.
func CheckConflict(detected, expected Role, conf float64) (bool, string) {
	if expected == RoleUnknown {
		return false, ""
	}
	if detected == expected {
		return false, ""
	}
	// Detecção com confiança baixa não gera conflito duro, só aviso.
	if conf < 0.55 {
		return true, fmt.Sprintf(
			"voz detectada como %q (conf %.0f%%) mas roteiro espera %q — confiança baixa, pode ser coincidência",
			detected.RolePT(), conf*100, expected.RolePT())
	}
	// Conflitos de gênero são mais sérios
	if genderOf(detected) != genderOf(expected) {
		return true, fmt.Sprintf(
			"GÊNERO DIVERGENTE: voz soa como %q (conf %.0f%%) mas roteiro diz %q. Se o personagem é menino dublado por seiyuu feminina, marcar --fix-roles para ajustar a classificação",
			detected.RolePT(), conf*100, expected.RolePT())
	}
	return true, fmt.Sprintf(
		"idade divergente: voz soa como %q (conf %.0f%%) mas roteiro diz %q",
		detected.RolePT(), conf*100, expected.RolePT())
}

func genderOf(r Role) string {
	switch r {
	case RoleMan, RoleOldMan, RoleBoy:
		return "m"
	case RoleWoman, RoleOldWoman, RoleGirl:
		return "f"
	}
	return "?"
}
