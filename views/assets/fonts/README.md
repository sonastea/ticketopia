# Fonts

## Manrope

Variable Manrope font, weights 400–800, sourced from the Google Fonts repository:
https://github.com/google/fonts/tree/main/ofl/manrope

Redistributed under the SIL Open Font License in `OFL.txt`.

## Bricolage Grotesque

Display face for page titles, event titles and date numbers only. Sourced on
2026-10-23 from the Google Fonts repository (Version 1.001):
https://github.com/google/fonts/tree/main/ofl/bricolagegrotesque

`bricolage-grotesque-latin.woff2` is a fontTools derivative of
`BricolageGrotesque[opsz,wdth,wght].ttf`: width pinned to 100, weight limited to
600–800 with the optical-size axis (12–96) retained, then subset to Google Fonts'
Latin range with kerning, case, and tabular/lining figure features:

```sh
fonttools varLib.instancer BricolageGrotesque[opsz,wdth,wght].ttf wdth=100 wght=600:800 opsz=12:96 -o inst.ttf
pyftsubset inst.ttf --unicodes="U+0000-00FF,U+0131,U+0152-0153,U+02BB-02BC,U+02C6,U+02DA,U+02DC,U+0304,U+0308,U+0329,U+2000-206F,U+20AC,U+2122,U+2191,U+2193,U+2212,U+2215,U+FEFF,U+FFFD" \
  --layout-features="kern,liga,calt,ccmp,locl,mark,mkmk,case,tnum,lnum,pnum" --flavor=woff2 --output-file=bricolage-grotesque-latin.woff2
```

Redistributed under the SIL Open Font License in `bricolage-grotesque-OFL.txt`.

## Google Sans

Google Sans Medium (500), Latin WOFF2, used only for the Google sign-in CTA.
Downloaded from the Google Fonts CSS API on 2026-10-06:
https://fonts.googleapis.com/css2?family=Google+Sans:wght@500&display=swap

Asset source:
https://fonts.gstatic.com/s/googlesans/v71/4Ua_rENHsxJlGDuGo1OIlJfC6l_24rlCK1Yo_Iqcsih3SAyH6cAwhX9RFD48TE63OOYKtrw2IJllpyk.woff2

Redistributed under the SIL Open Font License in `google-sans-OFL.txt`.

## Provider imagery

`../google-g.png` is Google's unmodified gradient G, downloaded on 2026-10-06
from https://developers.google.com/static/identity/images/g-logo.png for the
branded sign-in CTA. Its use follows Google's sign-in branding guidelines:
https://developers.google.com/identity/branding-guidelines

Event photography is supplied at runtime by Ticketmaster through normalized
`Event.Images` records; no photography is copied into this asset directory.
