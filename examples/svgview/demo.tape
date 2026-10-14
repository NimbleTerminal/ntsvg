# VHS tape for the svgview example.
# Generate with:  task vhs
#
# Note: VHS does not render Kitty graphics — this captures the
# half-block (Glyph) path only. Record Kitty-mode demos by hand.

Output demo.gif

Set FontSize 16
Set Width 1000
Set Height 640
Set Padding 20

Type "./bin/ntsvg-svgview" Enter
Sleep 2s

# Zoom into the rasterized sample.
Type "+" Sleep 600ms
Type "+" Sleep 600ms
Right Sleep 400ms
Down Sleep 600ms
Type "0" Sleep 800ms

# Generate a chart on the fly and view it.
Type "c" Sleep 1500ms

# Inspect the InfoMode summary.
Type "m" Sleep 1500ms
Type "m" Sleep 1s

Type "q"
