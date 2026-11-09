{
  pkgs,
  src ? ../.,
}:

{
  inherit src;

  pname = "gotk4-imageview";
  modules = ./gomod2nix.toml;
  subPackages = [ "cmd/gotk4-imageview" ];

  buildInputs =
    pkgs: with pkgs; [
      libadwaita
    ];

  files = { };
}
