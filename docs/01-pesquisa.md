# 01 — Pesquisa: ferramenta de dublagem + naturalidade

Data: 2026-07-31

## Ferramenta de TTS/dublagem escolhida

**OmniVoice** (`k2-fsa/OmniVoice`, Apache-2.0) — é o motor usado pelo
`~/projetos/aiuto_trend_producer` (`modules/omnivoice_narrator.py`), conforme
pedido. Fontes: HF model card, GitHub k2-fsa/OmniVoice, paper arXiv:2604.00688.

Fatos relevantes para o design:

- **600+ idiomas** incluindo português — ideal p/ dublar JA→PT no mesmo modelo.
- **Clonagem zero-shot**: ref_audio de **3–10 s** (acima disso degrada a
  qualidade e deixa mais lento). Referência no **mesmo idioma** da fala-alvo é
  o ideal; clonagem cross-lingual (voz JA falando PT) funciona mas é o caso
  "não ideal" que vamos usar.
- **RTF tão baixo quanto 0.025 (40× real)** — em CPU com float32 será maior,
  mas a inferência é leve (arquitetura diffusion-LM compacta, ~0.8B).
- **Voice Design**: dá para controlar `gender`, `age`, `pitch` por texto.
  Útil como **bandaid de consistência**: se o perfil de voz do personagem
  conflitar com o roteiro, podemos gerar dublagem por atributos como fallback.
- **Cache de prompt de clonagem** (`create_voice_clone_prompt`): codifica a voz
  do personagem **uma vez** e reutiliza em todas as falas dele — grande
  otimização de tempo (não re-encoda o ref em cada linha).
- CPU: `device_map="cpu"`, `dtype=float32` (regra do projeto).

Alternativa XTTS v2 (Coqui) também existe no motor, mas OmniVoice é o padrão
e mais rápido.

## Naturalidade da dublagem (melhores práticas — fontes)

Fontes: TikTok Audio "Lip Sync Timing", dubright.ai, HappyScribe "Dubbing
Techniques and Best Practices", TACL "Dubbing in Practice" (MIT), mkanime.ai.

Regras que o pipeline implementa:

1. **Isochrony**: a fala dublada deve caber na janela de tempo da fala
   original (boca abre → voz começa; boca fecha → voz termina). Usamos
   timestamps do ASR+VAD da fala original como a janela de cada fala.
2. **Não apressar**: "condense or expand lines as needed, ensuring the dubbed
   speech fits seamlessly without sounding rushed or awkward". Decisão do
   projeto: **não acelerar/stretch** a fala dublada (padrão `--stretch-max 0`).
   Se a linha não couber, reportar e preservar a próxima fala (sem atropelo).
3. **Sem atropelar falas**: ordem de mixagem respeita o início de cada fala;
   se duas falas originais se sobrepõem, a segunda é deslocada para o próximo
   vão livre e o evento vai para o relatório.
4. **Identidade vocal estável por personagem**: uma voz (clone) por cluster de
   falante, definido na diarização + perfil de voz.
5. **Limpeza de fundo vs personagens**: separação demucs (`vocals` × `other`).
   O fundo fica como "bed" com **ducking** durante as falas; as falas dubladas
   vêm limpas (clone de trechos de alta relação sinal/ruído do `vocals`).
6. **Mistura/ducking**: música/SFX baixa durante a fala, retorna no silêncio.
   Masterização final com loudnorm **-14 LUFS** (padrão streaming).
7. **Localização**: tradução com adaptação natural (pt), não literal.

## Validação de voz × roteiro (o "bandaid")

Pedido: um algoritmo que confira se a voz do personagem X é condizente com o
papel no roteiro (ex.: evitar chamar um menino de mulher).

Abordagem (implementada no pacote `voice`):
- **Perfil acústico** por cluster de falante: F0 (autocorrelação), centroide
  espectral, HNR (harmonic-to-noise), energia — tudo calculado em Go.
- **Classificação** sexo+idade: menino / menina / homem / mulher / idoso /
  idosa (faixas de F0 + atributos espectrais + HNR).
- **Cruzamento com o script**: se o roteiro (SRT com tags de falante ou JSON)
  declara o papel, comparamos com o perfil. Mismatch → flag `conflict` e
  sugestão; com `--fix-roles` o perfil acústico vence quando a confiança é
  alta. Nada disso trava o pipeline — só sinaliza e registra no relatório.

## Custos/estimativa (fontes e premissas)

Ver `docs/04-resultados.md` — preenchido após a medição real no teste.
