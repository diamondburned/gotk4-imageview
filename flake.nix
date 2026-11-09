{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs?ref=nixos-unstable";
    nixpkgs-gotk4.url = "github:NixOS/nixpkgs?ref=nixos-26.05";
    gotk4-nix.url = "github:diamondburned/gotk4-nix/main";
    flake-utils.url = "github:numtide/flake-utils";

    gomod2nix = {
      url = "github:nix-community/gomod2nix";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.flake-utils.follows = "flake-utils";
    };
  };

  outputs =
    {
      self,
      nixpkgs,
      nixpkgs-gotk4,
      gotk4-nix,
      gomod2nix,
      flake-utils,
    }:

    let
      baseFunc =
        pkgs:
        import ./nix/base.nix {
          inherit pkgs;
          src = self;
        };
    in
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        pkgs-gotk4 = (import nixpkgs-gotk4 {
          inherit system;
          overlays = [
            gomod2nix.overlays.default
            gotk4-nix.overlays.patchelf
          ];
        });
        go = pkgs.go_1_27;
        appPkg = gotk4-nix.lib.mkPackage {
          pkgs = pkgs-gotk4;
          inherit go;
          base = baseFunc pkgs-gotk4;
          version = self.rev or "dev";
        };
      in
      {
        devShells.default = gotk4-nix.lib.mkShell {
          base.pname = "gotk4-imageview";
          pkgs = pkgs-gotk4;

          buildInputs = with pkgs-gotk4; [
            gobject-introspection
            glib
            graphene
            gtk4
            vulkan-headers
            libadwaita
            (pkgs-gotk4.gomod2nix.override { inherit go; })
          ];

          packages = [
            pkgs.clang
            self.formatter.${system}
          ];

          inherit go;
          inherit (pkgs) gopls gotools;

          shellHook = ''
            export CC=clang # for speed
          '';
        };

        packages.default = appPkg;

        checks = nixpkgs.lib.optionalAttrs pkgs.stdenv.hostPlatform.isLinux {
          wayland-hidpi = pkgs.callPackage ./tests/wayland-hidpi {
            inherit self;
            gotk4-imageview = appPkg;
          };
        };

        formatter = pkgs.nixfmt;
      }
    );
}
