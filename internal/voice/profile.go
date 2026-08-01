// Classificação de sexo+idade a partir das features acústicas,
// e validação contra o roteiro (bandaid anti-confusão).
package voice

import (
	"fmt"
	"math"
	"strings"
)

// Role é a classificação normalizada: boy/girl/man/woman/oldman/oldwoman.
type Role string

const (
	RoleBoy     Role = "boy"
	RoleGirl    Role = "girl"
	RoleMan     Role = "man"
	RoleWoman   Role = "woman"
	RoleOldMan  Role = "oldman"
	RoleOldWoman Role = "oldwoman"
	RoleUnknown Role = "unknown"
)

// NormalizeRole converte rótulos pt/en livres para um Role.
func NormalizeRole(s string) Role {
	low := strings.ToLower(strings.TrimSpace(s))
	switch low {
	case "boy", "menino", "garoto", "garotinho", "crianca-m", "criança-m", "masc-crianca", "menino-m":
		return RoleBoy
	case "girl", "menina", "garota", "garotinha", "crianca-f", "criança-f", "fem-crianca", "menina-f":
		return RoleGirl
	case "man", "homem", "adulto-m", "masculino", "rapaz":
		return RoleMan
	case "woman", "mulher", "adulta-f", "feminino", "moça", "moca":
		return RoleWoman
	case "oldman", "idoso", "velho", "senhor", "avô", "avo":
		return RoleOldMan
	case "oldwoman", "idosa", "velha", "senhora", "avó", "vo":
		return RoleOldWoman
	default:
		return RoleUnknown
	}
}

// RolePT devolve o rótulo em português para relatório.
func (r Role) RolePT() string {
	switch r {
	case RoleBoy:
		return "menino"
	case RoleGirl:
		return "menina"
	case RoleMan:
		return "homem"
	case RoleWoman:
		return "mulher"
	case RoleOldMan:
		return "idoso"
	case RoleOldWoman:
		return "idosa"
	default:
		return "desconhecido"
	}
}

type classSpec struct {
	role  Role
	f0mu  float64 // média do log2(F0)
	f0sig float64
	hnr   float64
	hnrSig float64
	cen   float64
	cenSig float64
	pri   float64
}

// Tabela de referência (valores da literatura de fonética aplicados a
// anime — vozes japonesas, F0 médio). Ajustável conforme resultados.
var classes = []classSpec{
	{RoleOldMan, 6.80, 0.18, 4, 3, 800, 250, 0.15},   // ~111 Hz
	{RoleMan, 7.00, 0.25, 8, 3, 1100, 350, 0.20},     // ~128 Hz
	{RoleOldWoman, 7.50, 0.18, 5, 3, 1400, 350, 0.15}, // ~181 Hz
	{RoleWoman, 7.60, 0.20, 9, 3, 1700, 400, 0.20},   // ~194 Hz
	{RoleBoy, 8.00, 0.25, 12, 3, 2300, 450, 0.15},    // ~256 Hz
	{RoleGirl, 8.35, 0.22, 11, 3, 2600, 450, 0.15},   // ~326 Hz
}

// Profile é o resultado da análise de um trecho de voz.
type Profile struct {
	Role      Role    `json:"role"`
	Gender    string  `json:"gender"`
	Age       string  `json:"age"`
	Conf      float64 `json:"conf"`
	F0Med     float64 `json:"f0_med"`
	F0IQR     float64 `json:"f0_iqr"`
	F0Lo      float64 `json:"f0_lo"`
	F0Hi      float64 `json:"f0_hi"`
	HNR       float64 `json:"hnr"`
	Centroid  float64 `json:"centroid"`
	VoicedR   float64 `json:"voiced_ratio"`
	Samples   int     `json:"samples"`
}

// Analyze classifica um arquivo WAV mono 16k (ou um slice de samples).
func Analyze(samples []float64, sr int) (*Profile, error) {
	f0s, hnrs, cents, voicedR := TrackFeatures(samples, sr)
	if len(f0s) < 8 {
		return nil, fmt.Errorf("pouca voz detectada (%d frames voiced)", len(f0s))
	}
	f0Med, f0IQR, f0Lo, f0Hi := stats(f0s)
	hnr, _, _, _ := stats(hnrs)
	cent, _, _, _ := stats(cents)
	if cent <= 0 {
		cent = 1200
	}
	role, conf := classify(f0Med, hnr, cent)

	p := &Profile{
		Role:     role,
		Gender:   "unknown",
		Age:      "unknown",
		Conf:     conf,
		F0Med:    round2(f0Med),
		F0IQR:    round2(f0IQR),
		F0Lo:     round2(f0Lo),
		F0Hi:     round2(f0Hi),
		HNR:      round2(hnr),
		Centroid: round2(cent),
		VoicedR:  round2(voicedR),
		Samples:  len(f0s),
	}
	switch role {
	case RoleMan, RoleOldMan, RoleBoy:
		p.Gender = "male"
	case RoleWoman, RoleOldWoman, RoleGirl:
		p.Gender = "female"
	}
	switch role {
	case RoleBoy, RoleGirl:
		p.Age = "child"
	case RoleMan, RoleWoman:
		p.Age = "adult"
	case RoleOldMan, RoleOldWoman:
		p.Age = "elderly"
	}
	return p, nil
}

// AnalyzeFile lê o wav 16k mono e analisa.
func AnalyzeFile(path string) (*Profile, error) {
	s, err := ReadMono(path)
	if err != nil {
		return nil, err
	}
	if len(s) < 16000 {
		return nil, fmt.Errorf("áudio curto demais: %s", path)
	}
	return Analyze(s, 16000)
}

// classify pontua cada classe (gaussiana) e devolve a melhor + confiança
// (softmax normalizada).
func classify(f0med, hnr, cent float64) (Role, float64) {
	logf0 := math.Log2(f0med)
	scores := make([]float64, len(classes))
	best := -math.MaxFloat64
	bestIdx := 0
	for i, c := range classes {
		s := math.Log(c.pri)
		s -= sq((logf0-c.f0mu)/c.f0sig) * 0.5
		s -= sq((hnr-c.hnr)/c.hnrSig) * 0.5
		s -= sq((cent-c.cen)/c.cenSig) * 0.5
		scores[i] = s
		if s > best {
			best = s
			bestIdx = i
		}
	}
	var sum float64
	for _, s := range scores {
		sum += math.Exp(s - best)
	}
	conf := 1.0 / sum
	if conf > 0.99 {
		conf = 0.99
	}
	return classes[bestIdx].role, conf
}

func sq(v float64) float64 { return v * v }

func round2(v float64) float64 { return math.Round(v*100) / 100 }
