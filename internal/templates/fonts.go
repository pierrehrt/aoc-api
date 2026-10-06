package templates

import "html/template"

// FontFace is one @font-face the shell declares (AOC-075): IBM Plex, served from our own assets so the
// first paint waits on no third party (DECISIONS.md 2026-10-06). The files are IBM's own split woff2,
// unmodified, from @ibm/plex-sans 1.1.0 and @ibm/plex-mono 2.5.0 (web/src/fonts/, with the SIL OFL
// beside them; "Plex" is a Reserved Font Name, so no subset of ours is made). Each Range is IBM's
// unicode-range for that file: a browser downloads a file only when the page draws one of its
// characters. The weights are the ones Google served before (Sans 400–700, Mono 400–600); the subsets
// are Latin1 (the data and the interface), Latin2 (accented Latin) and Pi (the interface's arrows).
type FontFace struct {
	Family string
	Weight int
	File   string       // the asset's logical name, resolved by {{asset}}
	Range  template.CSS // IBM's own constant, never data
}

// FontFaces is generated from IBM's ibm-plex-{sans,mono}-all.css, kept as written there.
var FontFaces = []FontFace{
	{Family: "IBM Plex Sans", Weight: 400, File: "IBMPlexSans-Regular-Latin1.woff2",
		Range: "U+0000, U+000D, U+0020-007E, U+00A0-00A3, U+00A4-00FF, U+0131, U+0152-0153, U+02C6, U+02DA, U+02DC, U+2013-2014, U+2018-201A, U+201C-201E, U+2020-2022, U+2026, U+2030, U+2039-203A, U+2044, U+2074, U+20AC, U+2122, U+2212, U+FB01-FB02"},
	{Family: "IBM Plex Sans", Weight: 400, File: "IBMPlexSans-Regular-Latin2.woff2",
		Range: "U+0100-024F, U+0259, U+1E00-1EFF, U+20A0-20AB, U+20AD-20CF, U+2C60-2C7F, U+A720-A7FF, U+FB01-FB02"},
	{Family: "IBM Plex Sans", Weight: 400, File: "IBMPlexSans-Regular-Pi.woff2",
		Range: "U+0E3F, U+2032-2033, U+2070, U+2075-2079, U+2080-2081, U+2083, U+2085-2089, U+2113, U+2116, U+2126, U+212E, U+2150-2151, U+2153-215E, U+2190-2199, U+21A9-21AA, U+21B0-21B3, U+21B6-21B7, U+21BA-21BB, U+21C4, U+21C6, U+2202, U+2206, U+220F, U+2211, U+221A, U+221E, U+222B, U+2248, U+2260, U+2264-2265, U+25CA, U+2713, U+274C, U+2B0E-2B11, U+EBE1-EBE7, U+ECE0, U+EFCC"},
	{Family: "IBM Plex Sans", Weight: 500, File: "IBMPlexSans-Medium-Latin1.woff2",
		Range: "U+0000, U+000D, U+0020-007E, U+00A0-00A3, U+00A4-00FF, U+0131, U+0152-0153, U+02C6, U+02DA, U+02DC, U+2013-2014, U+2018-201A, U+201C-201E, U+2020-2022, U+2026, U+2030, U+2039-203A, U+2044, U+2074, U+20AC, U+2122, U+2212, U+FB01-FB02"},
	{Family: "IBM Plex Sans", Weight: 500, File: "IBMPlexSans-Medium-Latin2.woff2",
		Range: "U+0100-024F, U+0259, U+1E00-1EFF, U+20A0-20AB, U+20AD-20CF, U+2C60-2C7F, U+A720-A7FF, U+FB01-FB02"},
	{Family: "IBM Plex Sans", Weight: 500, File: "IBMPlexSans-Medium-Pi.woff2",
		Range: "U+0E3F, U+2032-2033, U+2070, U+2075-2079, U+2080-2081, U+2083, U+2085-2089, U+2113, U+2116, U+2126, U+212E, U+2150-2151, U+2153-215E, U+2190-2199, U+21A9-21AA, U+21B0-21B3, U+21B6-21B7, U+21BA-21BB, U+21C4, U+21C6, U+2202, U+2206, U+220F, U+2211, U+221A, U+221E, U+222B, U+2248, U+2260, U+2264-2265, U+25CA, U+2713, U+274C, U+2B0E-2B11, U+EBE1-EBE7, U+ECE0, U+EFCC"},
	{Family: "IBM Plex Sans", Weight: 600, File: "IBMPlexSans-SemiBold-Latin1.woff2",
		Range: "U+0000, U+000D, U+0020-007E, U+00A0-00A3, U+00A4-00FF, U+0131, U+0152-0153, U+02C6, U+02DA, U+02DC, U+2013-2014, U+2018-201A, U+201C-201E, U+2020-2022, U+2026, U+2030, U+2039-203A, U+2044, U+2074, U+20AC, U+2122, U+2212, U+FB01-FB02"},
	{Family: "IBM Plex Sans", Weight: 600, File: "IBMPlexSans-SemiBold-Latin2.woff2",
		Range: "U+0100-024F, U+0259, U+1E00-1EFF, U+20A0-20AB, U+20AD-20CF, U+2C60-2C7F, U+A720-A7FF, U+FB01-FB02"},
	{Family: "IBM Plex Sans", Weight: 600, File: "IBMPlexSans-SemiBold-Pi.woff2",
		Range: "U+0E3F, U+2032-2033, U+2070, U+2075-2079, U+2080-2081, U+2083, U+2085-2089, U+2113, U+2116, U+2126, U+212E, U+2150-2151, U+2153-215E, U+2190-2199, U+21A9-21AA, U+21B0-21B3, U+21B6-21B7, U+21BA-21BB, U+21C4, U+21C6, U+2202, U+2206, U+220F, U+2211, U+221A, U+221E, U+222B, U+2248, U+2260, U+2264-2265, U+25CA, U+2713, U+274C, U+2B0E-2B11, U+EBE1-EBE7, U+ECE0, U+EFCC"},
	{Family: "IBM Plex Sans", Weight: 700, File: "IBMPlexSans-Bold-Latin1.woff2",
		Range: "U+0000, U+000D, U+0020-007E, U+00A0-00A3, U+00A4-00FF, U+0131, U+0152-0153, U+02C6, U+02DA, U+02DC, U+2013-2014, U+2018-201A, U+201C-201E, U+2020-2022, U+2026, U+2030, U+2039-203A, U+2044, U+2074, U+20AC, U+2122, U+2212, U+FB01-FB02"},
	{Family: "IBM Plex Sans", Weight: 700, File: "IBMPlexSans-Bold-Latin2.woff2",
		Range: "U+0100-024F, U+0259, U+1E00-1EFF, U+20A0-20AB, U+20AD-20CF, U+2C60-2C7F, U+A720-A7FF, U+FB01-FB02"},
	{Family: "IBM Plex Sans", Weight: 700, File: "IBMPlexSans-Bold-Pi.woff2",
		Range: "U+0E3F, U+2032-2033, U+2070, U+2075-2079, U+2080-2081, U+2083, U+2085-2089, U+2113, U+2116, U+2126, U+212E, U+2150-2151, U+2153-215E, U+2190-2199, U+21A9-21AA, U+21B0-21B3, U+21B6-21B7, U+21BA-21BB, U+21C4, U+21C6, U+2202, U+2206, U+220F, U+2211, U+221A, U+221E, U+222B, U+2248, U+2260, U+2264-2265, U+25CA, U+2713, U+274C, U+2B0E-2B11, U+EBE1-EBE7, U+ECE0, U+EFCC"},
	{Family: "IBM Plex Mono", Weight: 400, File: "IBMPlexMono-Regular-Latin1.woff2",
		Range: "U+0020-007E, U+00A0-00FF, U+0131, U+0152-0153, U+02C6, U+02DA, U+02DC, U+2013-2014, U+2018-201A, U+201C-201E, U+2020-2022, U+2026, U+2030, U+2039-203A, U+2044, U+20AC, U+2122, U+2212, U+FB01-FB02"},
	{Family: "IBM Plex Mono", Weight: 400, File: "IBMPlexMono-Regular-Latin2.woff2",
		Range: "U+0100-0101, U+0104-0130, U+0132-0151, U+0154-017F, U+018F, U+0192, U+01A0-01A1, U+01AF-01B0, U+01FA-01FF, U+0218-021B, U+0237, U+0259, U+1E80-1E85, U+1E9E, U+20A1, U+20A4, U+20A6, U+20A8-20AA, U+20AD-20AE, U+20B1-20B2, U+20B4-20B5, U+20B8-20BA, U+20BD, U+20BF"},
	{Family: "IBM Plex Mono", Weight: 400, File: "IBMPlexMono-Regular-Pi.woff2",
		Range: "U+03C0, U+0E3F, U+2000-200D, U+2010-2012, U+2015, U+2028-2029, U+202F, U+2032-2033, U+203E, U+205F, U+2070, U+2074-2079, U+2080-2089, U+2113, U+2116, U+2126, U+212E, U+2150-2151, U+2153-215E, U+2190-2199, U+21A9-21AA, U+21B0-21B3, U+21B6-21B7, U+21BA-21BB, U+21C4, U+21C6, U+2202, U+2206, U+220F, U+2211, U+2215, U+2219-221A, U+221E, U+222B, U+2236, U+2248, U+2260, U+2264-2265, U+2400-2421, U+2500-259F, U+25CA, U+2713, U+274C, U+2B0E-2B11, U+3000, U+FEFF, U+FFFD"},
	{Family: "IBM Plex Mono", Weight: 500, File: "IBMPlexMono-Medium-Latin1.woff2",
		Range: "U+0020-007E, U+00A0-00FF, U+0131, U+0152-0153, U+02C6, U+02DA, U+02DC, U+2013-2014, U+2018-201A, U+201C-201E, U+2020-2022, U+2026, U+2030, U+2039-203A, U+2044, U+20AC, U+2122, U+2212, U+FB01-FB02"},
	{Family: "IBM Plex Mono", Weight: 500, File: "IBMPlexMono-Medium-Latin2.woff2",
		Range: "U+0100-0101, U+0104-0130, U+0132-0151, U+0154-017F, U+018F, U+0192, U+01A0-01A1, U+01AF-01B0, U+01FA-01FF, U+0218-021B, U+0237, U+0259, U+1E80-1E85, U+1E9E, U+20A1, U+20A4, U+20A6, U+20A8-20AA, U+20AD-20AE, U+20B1-20B2, U+20B4-20B5, U+20B8-20BA, U+20BD, U+20BF"},
	{Family: "IBM Plex Mono", Weight: 500, File: "IBMPlexMono-Medium-Pi.woff2",
		Range: "U+03C0, U+0E3F, U+2000-200D, U+2010-2012, U+2015, U+2028-2029, U+202F, U+2032-2033, U+203E, U+205F, U+2070, U+2074-2079, U+2080-2089, U+2113, U+2116, U+2126, U+212E, U+2150-2151, U+2153-215E, U+2190-2199, U+21A9-21AA, U+21B0-21B3, U+21B6-21B7, U+21BA-21BB, U+21C4, U+21C6, U+2202, U+2206, U+220F, U+2211, U+2215, U+2219-221A, U+221E, U+222B, U+2236, U+2248, U+2260, U+2264-2265, U+2400-2421, U+2500-259F, U+25CA, U+2713, U+274C, U+2B0E-2B11, U+3000, U+FEFF, U+FFFD"},
	{Family: "IBM Plex Mono", Weight: 600, File: "IBMPlexMono-SemiBold-Latin1.woff2",
		Range: "U+0020-007E, U+00A0-00FF, U+0131, U+0152-0153, U+02C6, U+02DA, U+02DC, U+2013-2014, U+2018-201A, U+201C-201E, U+2020-2022, U+2026, U+2030, U+2039-203A, U+2044, U+20AC, U+2122, U+2212, U+FB01-FB02"},
	{Family: "IBM Plex Mono", Weight: 600, File: "IBMPlexMono-SemiBold-Latin2.woff2",
		Range: "U+0100-0101, U+0104-0130, U+0132-0151, U+0154-017F, U+018F, U+0192, U+01A0-01A1, U+01AF-01B0, U+01FA-01FF, U+0218-021B, U+0237, U+0259, U+1E80-1E85, U+1E9E, U+20A1, U+20A4, U+20A6, U+20A8-20AA, U+20AD-20AE, U+20B1-20B2, U+20B4-20B5, U+20B8-20BA, U+20BD, U+20BF"},
	{Family: "IBM Plex Mono", Weight: 600, File: "IBMPlexMono-SemiBold-Pi.woff2",
		Range: "U+03C0, U+0E3F, U+2000-200D, U+2010-2012, U+2015, U+2028-2029, U+202F, U+2032-2033, U+203E, U+205F, U+2070, U+2074-2079, U+2080-2089, U+2113, U+2116, U+2126, U+212E, U+2150-2151, U+2153-215E, U+2190-2199, U+21A9-21AA, U+21B0-21B3, U+21B6-21B7, U+21BA-21BB, U+21C4, U+21C6, U+2202, U+2206, U+220F, U+2211, U+2215, U+2219-221A, U+221E, U+222B, U+2236, U+2248, U+2260, U+2264-2265, U+2400-2421, U+2500-259F, U+25CA, U+2713, U+274C, U+2B0E-2B11, U+3000, U+FEFF, U+FFFD"},
}
