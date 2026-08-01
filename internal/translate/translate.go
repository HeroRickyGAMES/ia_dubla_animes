// Package translate traduz ja→pt usando o endpoint gratuito do Google
// Translate (client=gtx), com batching paralelo e limitação de requests.
package translate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const endpoint = "https://translate.googleapis.com/translate_a/single"

// Client traduz em lotes paralelos.
type Client struct {
	HTTP   *http.Client
	Workers int
	Delay  time.Duration // pausa entre lotes (respeito ao endpoint gratuito)
	MaxBatch int
}

func New() *Client {
	return &Client{
		HTTP:    &http.Client{Timeout: 20 * time.Second},
		Workers: 6,
		Delay:   150 * time.Millisecond,
		MaxBatch: 24,
	}
}

// Translate traduz uma fatia de textos.
func (c *Client) Translate(ctx context.Context, from, to string, texts []string) ([]string, error) {
	out := make([]string, len(texts))
	if len(texts) == 0 {
		return out, nil
	}
	// agrupa em lotes
	var batches [][]int
	for i := 0; i < len(texts); i += c.MaxBatch {
		end := i + c.MaxBatch
		if end > len(texts) {
			end = len(texts)
		}
		var idx []int
		for j := i; j < end; j++ {
			idx = append(idx, j)
		}
		batches = append(batches, idx)
	}

	jobs := make(chan []int)
	errs := make(chan error, len(batches))
	var wg sync.WaitGroup
	mu := sync.Mutex{}

	for w := 0; w < c.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				batch := make([]string, len(idx))
				for k, i := range idx {
					batch[k] = texts[i]
				}
				translated, err := c.translateBatch(ctx, from, to, batch)
				if err != nil {
					errs <- fmt.Errorf("lote %v: %w", idx, err)
				}
				mu.Lock()
				for k, i := range idx {
					if translated[k] != "" {
						out[i] = translated[k]
					}
				}
				mu.Unlock()
				time.Sleep(c.Delay)
			}
		}()
	}
	for _, b := range batches {
		jobs <- b
	}
	close(jobs)
	wg.Wait()
	close(errs)
	var nerr error
	for e := range errs {
		if nerr == nil {
			nerr = e
		} else {
			nerr = fmt.Errorf("%v; %w", nerr, e)
		}
	}
	if nerr != nil {
		return out, nerr
	}
	return out, nil
}

// translateBatch traduz uma lista de textos (um request por texto;
// o endpoint gtx não suporta múltiplos &q= de forma confiável).
func (c *Client) translateBatch(ctx context.Context, from, to string, texts []string) ([]string, error) {
	res := make([]string, len(texts))
	var fail error
	for i, t := range texts {
		txt, err := c.translateOne(ctx, from, to, t)
		if err != nil {
			if fail == nil {
				fail = fmt.Errorf("texto %d: %w", i, err)
			} else {
				fail = fmt.Errorf("%v; texto %d: %w", fail, i, err)
			}
			continue
		}
		res[i] = txt
		if i < len(texts)-1 {
			time.Sleep(c.Delay)
		}
	}
	if fail != nil {
		return res, fail
	}
	return res, nil
}

// translateOne traduz um único texto.
func (c *Client) translateOne(ctx context.Context, from, to, text string) (string, error) {
	q := url.Values{}
	q.Set("client", "gtx")
	q.Set("sl", from)
	q.Set("tl", to)
	q.Set("dt", "t")
	q.Set("q", text)
	u := endpoint + "?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64)")

	var resp *http.Response
	for attempt := 0; ; attempt++ {
		resp, err = c.HTTP.Do(req)
		if err == nil || attempt >= 2 {
			break
		}
		time.Sleep(time.Duration(attempt+1) * 700 * time.Millisecond)
	}
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}
	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return "", err
	}
	// estrutura: [ [[pt,ja,null,null,10], ...], null, "ja", ...]
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil || len(arr) == 0 {
		return "", fmt.Errorf("resposta inesperada")
	}
	var segs []json.RawMessage
	if err := json.Unmarshal(arr[0], &segs); err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, s := range segs {
		var pair []json.RawMessage
		if err := json.Unmarshal(s, &pair); err != nil || len(pair) == 0 {
			continue
		}
		var txt string
		if err := json.Unmarshal(pair[0], &txt); err == nil {
			sb.WriteString(txt)
		}
	}
	res := sb.String()
	if res == "" {
		return "", fmt.Errorf("tradução vazia")
	}
	return res, nil
}
