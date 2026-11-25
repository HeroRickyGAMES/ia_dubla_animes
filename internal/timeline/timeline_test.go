package timeline

import "testing"

// Regressão: a dublagem não pode continuar depois do fim da janela original
// só porque a fala seguinte está distante (causa da assincronia relatada).
func TestPlanRespeitaFimDaJanela(t *testing.T) {
	ls := []Line{
		{ID: 1, Start: 10.0, End: 12.0, Speaker: "spk_0", DubLen: 3.0},
		{ID: 2, Start: 30.0, End: 31.0, Speaker: "spk_0", DubLen: 1.0},
	}
	out := Plan(ls, 0.15)
	first := out[0]
	if first.Place+first.PlayLen > first.End+windowTol+1e-9 {
		t.Fatalf("fala 1 estourou a janela: place=%.2f play=%.2f end=%.2f flags=%v",
			first.Place, first.PlayLen, first.End, first.Flags)
	}
	if first.PlayLen >= first.DubLen {
		t.Fatalf("fala longa não foi limitada: play=%.2f dub=%.2f", first.PlayLen, first.DubLen)
	}
}

// Personagens diferentes não devem sobrepor vozes além de uma fração curta.
func TestPlanNaoSobrepoePersonagensDiferentes(t *testing.T) {
	ls := []Line{
		{ID: 1, Start: 10.0, End: 12.0, Speaker: "spk_0", DubLen: 2.3},
		{ID: 2, Start: 12.1, End: 13.0, Speaker: "spk_1", DubLen: 0.8},
	}
	out := Plan(ls, 0.15)
	gap := out[1].Place - (out[0].Place + out[0].PlayLen)
	if gap < -0.16 {
		t.Fatalf("personagens diferentes sobrepostos por %.2fs", -gap)
	}
}
