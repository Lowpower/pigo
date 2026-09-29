package theme

import "math"

// Oklab and OKHSL conversion. Port of Björn Ottosson's reference implementation
// (https://bottosson.github.io/posts/colorpicker/), MIT license:
//
// Copyright (c) 2021 Björn Ottosson. Permission is hereby granted, free of
// charge, to any person obtaining a copy of this software and associated
// documentation files, to deal in the Software without restriction, subject
// to the condition that the copyright notice and this permission notice appear
// in all copies or substantial portions of the Software. THE SOFTWARE IS
// PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND.

type vec3 [3]float64

type mat3 [3]vec3

func mul(m mat3, v vec3) vec3 {
	var out vec3
	for i := range m {
		out[i] = m[i][0]*v[0] + m[i][1]*v[1] + m[i][2]*v[2]
	}
	return out
}

func cube(v vec3) vec3 {
	return vec3{v[0] * v[0] * v[0], v[1] * v[1] * v[1], v[2] * v[2] * v[2]}
}

func cbrt3(v vec3) vec3 {
	return vec3{math.Cbrt(v[0]), math.Cbrt(v[1]), math.Cbrt(v[2])}
}

var (
	linearSrgbToLMS = mat3{
		{0.4122214694707629, 0.5363325372617349, 0.0514459932675022},
		{0.2119034958178251, 0.6806995506452344, 0.1073969535369405},
		{0.0883024591900564, 0.2817188391361215, 0.6299787016738222},
	}
	lmsToLab = mat3{
		{0.210454268309314, 0.793617774702305, -0.0040720430116193},
		{1.9779985324311684, -2.42859224204858, 0.450593709617411},
		{0.0259040424655478, 0.7827717124575296, -0.8086757549230774},
	}
	labToLMS = mat3{
		{1, 0.3963377773761749, 0.2158037573099136},
		{1, -0.1055613458156586, -0.0638541728258133},
		{1, -0.0894841775298119, -1.2914855480194092},
	}
	lmsToLinearSrgb = mat3{
		{4.0767416360759583, -3.3077115392580629, 0.2309699031821043},
		{-1.2684379732850315, 2.6097573492876882, -0.341319376002657},
		{-0.0041960761386756, -0.7034186179359362, 1.7076146940746117},
	}
)

// Per sRGB channel: the (a, b) half-plane where that channel clips first,
// and the polynomial for maximum saturation there.
var saturationFit = [3]struct {
	x, y float64
	k    [5]float64
}{
	{-1.8817031, -0.80936501, [5]float64{1.19086277, 1.76576728, 0.59662641, 0.75515197, 0.56771245}},
	{1.8144408, -1.19445267, [5]float64{0.73956515, -0.45954404, 0.08285427, 0.12541073, -0.14503204}},
	{0.13110758, 1.81333971, [5]float64{1.35733652, -0.00915799, -1.1513021, -0.50559606, 0.00692167}},
}

const (
	okhslK1 = 0.206
	okhslK2 = 0.03
	okhslK3 = (1 + okhslK1) / (1 + okhslK2)
)

func oklabToOkhslLightness(x float64) float64 {
	return 0.5 * (okhslK3*x - okhslK1 + math.Sqrt((okhslK3*x-okhslK1)*(okhslK3*x-okhslK1)+4*okhslK2*okhslK3*x))
}

func okhslToOklabLightness(x float64) float64 {
	return (x*x + okhslK1*x) / (okhslK3 * (x + okhslK2))
}

func linearToSrgb(value float64) float64 {
	if value > 0.0031308 {
		return 1.055*math.Pow(value, 1/2.4) - 0.055
	}
	return 12.92 * value
}

func srgbToLinear(value float64) float64 {
	if value <= 0.04045 {
		return value / 12.92
	}
	return math.Pow((value+0.055)/1.055, 2.4)
}

func oklabToLinearSrgb(lab vec3) vec3 {
	return mul(lmsToLinearSrgb, cube(mul(labToLMS, lab)))
}

func linearSrgbToOklab(rgb vec3) vec3 {
	return mul(lmsToLab, cbrt3(mul(linearSrgbToLMS, rgb)))
}

func rgbToOklab(c RGB) vec3 {
	return linearSrgbToOklab(vec3{
		srgbToLinear(float64(c.R) / 255),
		srgbToLinear(float64(c.G) / 255),
		srgbToLinear(float64(c.B) / 255),
	})
}

func linearSrgbToRgb(linear vec3) RGB {
	ch := func(v float64) int {
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		return int(math.Round(linearToSrgb(v) * 255))
	}
	return RGB{R: ch(linear[0]), G: ch(linear[1]), B: ch(linear[2])}
}

func oklabLightness(c RGB) float64 {
	return rgbToOklab(c)[0]
}

func lmsSlopes(a, b float64) vec3 {
	var out vec3
	for i := range labToLMS {
		out[i] = labToLMS[i][1]*a + labToLMS[i][2]*b
	}
	return out
}

func maxSaturation(a, b float64) float64 {
	channel := 2
	for i, fit := range saturationFit {
		if i == 2 || fit.x*a+fit.y*b > 1 {
			channel = i
			break
		}
	}
	k := saturationFit[channel].k
	saturation := k[0] + k[1]*a + k[2]*b + k[3]*a*a + k[4]*a*b
	weights := lmsToLinearSrgb[channel]
	slopes := lmsSlopes(a, b)
	var base vec3
	for i := range slopes {
		base[i] = 1 + saturation*slopes[i]
	}
	dot := func(values vec3) float64 {
		return weights[0]*values[0] + weights[1]*values[1] + weights[2]*values[2]
	}
	var cubes, first, second vec3
	for i := range base {
		cubes[i] = base[i] * base[i] * base[i]
		first[i] = 3 * slopes[i] * base[i] * base[i]
		second[i] = 6 * slopes[i] * slopes[i] * base[i]
	}
	f := dot(cubes)
	f1 := dot(first)
	f2 := dot(second)
	return saturation - (f*f1)/(f1*f1-0.5*f*f2)
}

func cusp(a, b float64) (float64, float64) {
	saturation := maxSaturation(a, b)
	lin := oklabToLinearSrgb(vec3{1, saturation * a, saturation * b})
	peak := lin[0]
	if lin[1] > peak {
		peak = lin[1]
	}
	if lin[2] > peak {
		peak = lin[2]
	}
	lightness := math.Cbrt(1 / peak)
	return lightness, lightness * saturation
}

func maxChroma(a, b, lightness, cuspL, cuspC float64) float64 {
	if lightness <= cuspL {
		return (cuspC * lightness) / cuspL
	}
	t := (cuspC * (lightness - 1)) / (cuspL - 1)
	slopes := lmsSlopes(a, b)
	var lms, cubes, first, second vec3
	for i := range slopes {
		lms[i] = lightness + t*slopes[i]
		cubes[i] = lms[i] * lms[i] * lms[i]
		first[i] = 3 * slopes[i] * lms[i] * lms[i]
		second[i] = 6 * slopes[i] * slopes[i] * lms[i]
	}
	dot := func(row, values vec3) float64 {
		return row[0]*values[0] + row[1]*values[1] + row[2]*values[2]
	}
	best := math.MaxFloat64
	for _, row := range lmsToLinearSrgb {
		f := dot(row, cubes) - 1
		f1 := dot(row, first)
		f2 := dot(row, second)
		u := f1 / (f1*f1 - 0.5*f*f2)
		step := math.MaxFloat64
		if u >= 0 {
			step = -f * u
		}
		if step < best {
			best = step
		}
	}
	return t + best
}

func chromaStops(l, a, b float64) (float64, float64, float64) {
	cuspL, cuspC := cusp(a, b)
	cMax := maxChroma(a, b, l, cuspL, cuspC)
	denom := math.Min(l*(cuspC/cuspL), (1-l)*(cuspC/(1-cuspL)))
	k := cMax / denom
	midS := 0.11516993 + 1/(7.4477897+4.1590124*b+a*(-2.19557347+1.75198401*b+a*(-2.13704948-10.02301043*b+a*(-4.24894561+5.38770819*b+4.69891013*a))))
	midT := 0.11239642 + 1/(1.6132032-0.68124379*b+a*(0.40370612+0.90148123*b+a*(-0.27087943+0.6122399*b+a*(0.00299215-0.45399568*b-0.14661872*a))))
	cMid := 0.9 * k * math.Sqrt(math.Sqrt(1/(1/math.Pow(l*midS, 4)+1/math.Pow((1-l)*midT, 4))))
	l04 := l * 0.4
	l08 := (1 - l) * 0.8
	c0 := math.Sqrt(1 / (1/(l04*l04) + 1/(l08*l08)))
	return c0, cMid, cMax
}

type okhsl struct {
	h, s, l float64
}

func okhslToRgb(hue, saturation, lightness float64) RGB {
	l := okhslToOklabLightness(lightness)
	lab := vec3{l, 0, 0}
	if l > 0 && l < 1 && saturation > 0 {
		angle := (2 * math.Pi * mod360(hue)) / 360
		a := math.Cos(angle)
		b := math.Sin(angle)
		c0, cMid, cMax := chromaStops(l, a, b)
		var chroma float64
		if saturation < 0.8 {
			t := 1.25 * saturation
			k1 := 0.8 * c0
			chroma = (t * k1) / (1 - (1-k1/cMid)*t)
		} else {
			t := 5 * (saturation - 0.8)
			k1 := (0.2 * cMid * cMid * 1.25 * 1.25) / c0
			chroma = cMid + (t*k1)/(1-(1-k1/(cMax-cMid))*t)
		}
		lab = vec3{l, chroma * a, chroma * b}
	}
	return linearSrgbToRgb(oklabToLinearSrgb(lab))
}

func rgbToOkhsl(c RGB) okhsl {
	lab := rgbToOklab(c)
	chroma := math.Hypot(lab[1], lab[2])
	lightness := oklabToOkhslLightness(lab[0])
	if chroma < 1e-9 || lightness <= 0 || lightness >= 1 {
		return okhsl{l: lightness}
	}
	hue := math.Mod(math.Atan2(lab[2], lab[1])*180/math.Pi+360, 360)
	c0, cMid, cMax := chromaStops(lab[0], lab[1]/chroma, lab[2]/chroma)
	var saturation float64
	if chroma < cMid {
		k1 := 0.8 * c0
		saturation = 0.8 * (chroma / (k1 + (1-k1/cMid)*chroma))
	} else {
		k1 := (0.2 * cMid * cMid * 1.25 * 1.25) / c0
		offset := chroma - cMid
		saturation = 0.8 + 0.2*(offset/(k1+(1-k1/(cMax-cMid))*offset))
	}
	if saturation < 0 {
		saturation = 0
	}
	if saturation > 1 {
		saturation = 1
	}
	return okhsl{h: hue, s: saturation, l: lightness}
}

func mod360(h float64) float64 {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	return h
}
