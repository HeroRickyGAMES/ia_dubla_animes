// Cálculo de features acústicas em Go (stdlib puro): FFT, autocorrelação,
// pitch (F0), HNR e centroide espectral. Sem dependências externas.
package voice

import (
	"math"
	"sort"
)

const (
	minF0   = 60.0
	maxF0   = 550.0
	frameMs = 25.0
	hopMs   = 10.0
)

// fft radix-2 (in-place). n deve ser potência de 2.
func fft(x []complex128, invert bool) {
	n := len(x)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}
	for length := 2; length <= n; length <<= 1 {
		ang := -2 * math.Pi / float64(length)
		if invert {
			ang = -ang
		}
		wlen := complex(math.Cos(ang), math.Sin(ang))
		for i := 0; i < n; i += length {
			w := complex(1, 0)
			for j := 0; j < length/2; j++ {
				u := x[i+j]
				v := x[i+j+length/2] * w
				x[i+j] = u + v
				x[i+j+length/2] = u - v
				w *= wlen
			}
		}
	}
	if invert {
		for i := 0; i < n; i++ {
			x[i] /= complex(float64(n), 0)
		}
	}
}

func mean(s []float64) float64 {
	if len(s) == 0 {
		return 0
	}
	var sum float64
	for _, v := range s {
		sum += v
	}
	return sum / float64(len(s))
}

// Features de um frame de áudio.
type frameFeat struct {
	F0       float64
	HNR      float64
	Voiced   bool
	Centroid float64
	RMS      float64
}

// analyzeFrame calcula F0+HNR por autocorrelação e centroide por FFT.
func analyzeFrame(frame []float64, sr int) frameFeat {
	f := frameFeat{RMS: rms(frame)}
	// remove DC
	x := make([]float64, len(frame))
	copy(x, frame)
	xmean := mean(x)
	for i := range x {
		x[i] -= xmean
	}

	// ---- autocorrelação p/ pitch + HNR ----
	minLag := sr / int(maxF0)
	maxLag := sr / int(minF0)
	if maxLag >= len(x) {
		maxLag = len(x) - 1
	}
	if minLag < 1 {
		minLag = 1
	}
	bestLag, bestVal := -1, 0.0
	for lag := minLag; lag <= maxLag && lag < len(x); lag++ {
		var acc float64
		for i := 0; i < len(x)-lag; i++ {
			acc += x[i] * x[i+lag]
		}
		if acc > bestVal {
			bestVal = acc
			bestLag = lag
		}
	}
	r0 := 0.0
	for _, v := range x {
		r0 += v * v
	}
	if r0 <= 1e-12 {
		return f
	}
	norm := bestVal / r0
	if norm > 0.40 && bestLag > 0 {
		f.F0 = float64(sr) / float64(bestLag)
		f.Voiced = f.F0 >= minF0 && f.F0 <= maxF0
		if norm >= 1 {
			f.HNR = 40
		} else {
			f.HNR = 10 * math.Log10(norm/(1-norm))
		}
	}

	// ---- centroide espectral via FFT ----
	n := 2048
	fftIn := make([]complex128, n)
	for i := range fftIn {
		if i < len(frame) {
			w := 0.54 - 0.46*math.Cos(2*math.Pi*float64(i)/float64(len(frame)-1))
			fftIn[i] = complex(frame[i]*w, 0)
		}
	}
	fft(fftIn, false)
	var num, den float64
	for i := 0; i <= n/2; i++ {
		mag := math.Hypot(real(fftIn[i]), imag(fftIn[i]))
		freq := float64(i) * float64(sr) / float64(n)
		num += freq * mag
		den += mag
	}
	if den > 1e-9 {
		f.Centroid = num / den
	}
	return f
}

func rms(x []float64) float64 {
	var acc float64
	for _, v := range x {
		acc += v * v
	}
	return math.Sqrt(acc / float64(len(x)))
}

func quantile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(q * float64(len(sorted)))
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// TrackFeatures percorre o sinal em frames e agrega as features.
func TrackFeatures(samples []float64, sr int) (f0s, hnrs, cents []float64, voicedRatio float64) {
	frameLen := int(float64(sr) * frameMs / 1000)
	hopLen := int(float64(sr) * hopMs / 1000)
	if frameLen < 2 || hopLen < 1 {
		return nil, nil, nil, 0
	}
	totalFrames := 0
	for start := 0; start+frameLen <= len(samples); start += hopLen {
		frame := samples[start : start+frameLen]
		if rms(frame) < 1e-4 {
			continue
		}
		totalFrames++
		feat := analyzeFrame(frame, sr)
		if feat.Voiced && feat.F0 > 0 {
			f0s = append(f0s, feat.F0)
			hnrs = append(hnrs, feat.HNR)
		}
		cents = append(cents, feat.Centroid)
	}
	if totalFrames == 0 {
		return nil, nil, nil, 0
	}
	voicedRatio = float64(len(f0s)) / float64(totalFrames)
	return f0s, hnrs, cents, voicedRatio
}

// Stats agrega um slice em estatísticas básicas.
func stats(s []float64) (median, iqr, lo, hi float64) {
	if len(s) == 0 {
		return 0, 0, 0, 0
	}
	cp := make([]float64, len(s))
	copy(cp, s)
	sort.Float64s(cp)
	median = quantile(cp, 0.5)
	lo = quantile(cp, 0.1)
	hi = quantile(cp, 0.9)
	iqr = quantile(cp, 0.75) - quantile(cp, 0.25)
	return
}
