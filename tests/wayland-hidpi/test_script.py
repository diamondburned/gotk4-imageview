start_all()
machine.wait_for_unit("multi-user.target")

# Prepare runtime directory and test images for user alice (UID 1000).
machine.succeed("install -d -m 0700 -o alice -g users /run/user/1000")

# Generate a 400x300 test PNG with 4 distinct quadrant colors:
#   Top-Left:     Pure Red   (255, 0, 0)
#   Top-Right:    Pure Green (0, 255, 0)
#   Bottom-Left:  Pure Blue  (0, 0, 255)
#   Bottom-Right: Pure White (255, 255, 255)
machine.succeed(
    """python3 - << 'EOF'
from PIL import Image

img = Image.new("RGBA", (400, 300))
px = img.load()
for y in range(300):
    for x in range(400):
        if x < 200 and y < 150:
            px[x, y] = (255, 0, 0, 255)
        elif x >= 200 and y < 150:
            px[x, y] = (0, 255, 0, 255)
        elif x < 200 and y >= 150:
            px[x, y] = (0, 0, 255, 255)
        else:
            px[x, y] = (255, 255, 255, 255)
img.save("/tmp/quadrants.png")
EOF"""
)

with subtest("Start headless Sway Wayland compositor at HiDPI scale 2"):
    machine.succeed(
        """cat > /tmp/sway.cfg << 'EOF'
output HEADLESS-1 resolution 1920x1080 scale 2
default_border none
default_floating_border none
bar {
    mode invisible
}
EOF"""
    )
    machine.succeed(
        "su - alice -c 'XDG_RUNTIME_DIR=/run/user/1000 "
        "WLR_BACKENDS=headless WLR_LIBINPUT_NO_DEVICES=1 WLR_RENDERER=pixman "
        "sway -c /tmp/sway.cfg >/tmp/sway.log 2>&1 &'"
    )
    machine.wait_until_succeeds("test -S /run/user/1000/wayland-1")

    # Verify Sway output scale is 2.0 (1920x1080 physical, 960x540 logical).
    scale = machine.succeed(
        "su - alice -c 'XDG_RUNTIME_DIR=/run/user/1000 "
        "SWAYSOCK=$(ls /run/user/1000/sway-ipc.*.sock | head -n1) "
        "swaymsg -t get_outputs | jq -r \".[0].scale\"'"
    ).strip()
    assert scale == "2.0" or scale == "2", f"expected HiDPI scale 2, got {scale}"

with subtest("Launch gotk4-imageview on HiDPI Wayland and verify quadrant colors"):
    machine.succeed(
        "su - alice -c 'XDG_RUNTIME_DIR=/run/user/1000 "
        "WAYLAND_DISPLAY=wayland-1 GSK_RENDERER=cairo "
        "gotk4-imageview /tmp/quadrants.png >/tmp/imageview.log 2>&1 &'"
    )

    # Wait until the window is mapped in Sway's tree.
    machine.wait_until_succeeds(
        "su - alice -c 'XDG_RUNTIME_DIR=/run/user/1000 "
        "SWAYSOCK=$(ls /run/user/1000/sway-ipc.*.sock | head -n1) "
        "swaymsg -t get_tree | grep -q quadrants.png'"
    )

    # Wait until the rendered output contains the red, green, and blue quadrants.
    machine.wait_until_succeeds(
        """su - alice -c 'XDG_RUNTIME_DIR=/run/user/1000 WAYLAND_DISPLAY=wayland-1 grim /tmp/shot-initial.png' && python3 - << 'EOF'
from PIL import Image
im = Image.open("/tmp/shot-initial.png")
assert im.size == (1920, 1080), f"unexpected screenshot size: {im.size}"
# Center of screen is (960, 540). Sample inside the 4 quadrants around the center:
tl = im.getpixel((800, 420))
tr = im.getpixel((1120, 420))
bl = im.getpixel((800, 660))
br = im.getpixel((1120, 660))
assert tl[0] > 200 and tl[1] < 40 and tl[2] < 40, f"top-left not red: {tl}"
assert tr[1] > 200 and tr[0] < 40 and tr[2] < 40, f"top-right not green: {tr}"
assert bl[2] > 200 and bl[0] < 40 and bl[1] < 40, f"bottom-left not blue: {bl}"
assert br[0] > 200 and br[1] > 200 and br[2] > 200, f"bottom-right not white: {br}"
EOF"""
    )
    machine.copy_from_machine("/tmp/shot-initial.png", "screenshots")

with subtest("Rotate 90 degrees clockwise via keyboard and verify rotated quadrants"):
    machine.succeed(
        "su - alice -c 'XDG_RUNTIME_DIR=/run/user/1000 WAYLAND_DISPLAY=wayland-1 wtype r'"
    )
    # After 90 deg CW rotation:
    #   Top-Left becomes old Bottom-Left (Blue)
    #   Top-Right becomes old Top-Left (Red)
    #   Bottom-Right becomes old Top-Right (Green)
    #   Bottom-Left becomes old Bottom-Right (White)
    machine.wait_until_succeeds(
        """su - alice -c 'XDG_RUNTIME_DIR=/run/user/1000 WAYLAND_DISPLAY=wayland-1 grim /tmp/shot-rotated.png' && python3 - << 'EOF'
from PIL import Image
im = Image.open("/tmp/shot-rotated.png")
tl = im.getpixel((840, 380))
tr = im.getpixel((1080, 380))
bl = im.getpixel((840, 700))
br = im.getpixel((1080, 700))
assert tl[2] > 200 and tl[0] < 40, f"rotated top-left not blue: {tl}"
assert tr[0] > 200 and tr[1] < 40, f"rotated top-right not red: {tr}"
assert bl[0] > 200 and bl[1] > 200 and bl[2] > 200, f"rotated bottom-left not white: {bl}"
assert br[1] > 200 and br[0] < 40, f"rotated bottom-right not green: {br}"
EOF"""
    )
    machine.copy_from_machine("/tmp/shot-rotated.png", "screenshots")

with subtest("Negative case: corrupted file logs error without crashing"):
    machine.succeed("printf 'not-a-valid-image' > /tmp/corrupt.png")
    machine.succeed(
        "su - alice -c 'XDG_RUNTIME_DIR=/run/user/1000 "
        "WAYLAND_DISPLAY=wayland-1 GSK_RENDERER=cairo "
        "gotk4-imageview /tmp/corrupt.png >/tmp/corrupt.log 2>&1 &'"
    )
    machine.wait_until_succeeds("grep -q 'failed to load /tmp/corrupt.png' /tmp/corrupt.log")
