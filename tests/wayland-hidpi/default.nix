{
  pkgs,
  self,
  gotk4-imageview,
}:

pkgs.testers.runNixOSTest {
  name = "gotk4-imageview-wayland-hidpi";

  nodes.machine =
    { pkgs, ... }:
    {
      # Create an unprivileged desktop user for the Wayland session.
      users.users.alice = {
        isNormalUser = true;
        uid = 1000;
        extraGroups = [
          "video"
          "render"
        ];
      };

      # Headless Sway Wayland session with grim for output capture and wtype for
      # synthetic keyboard input.
      programs.sway.enable = true;

      environment.systemPackages = with pkgs; [
        gotk4-imageview
        sway
        grim
        wtype
        jq
        (python3.withPackages (ps: [ ps.pillow ]))
      ];

      fonts.packages = with pkgs; [
        dejavu_fonts
      ];

      documentation.enable = false;
      virtualisation.memorySize = 2048;
    };

  testScript = builtins.readFile ./test_script.py;
}
