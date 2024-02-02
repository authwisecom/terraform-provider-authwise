with import <nixpkgs> {};

stdenv.mkDerivation {

  name = "terraform-provider";

  buildInputs = with pkgs; [
    jetbrains.goland
  ];

  shellHook = ''
    goland .
    exit
  '';

  hardeningDisable = [ "fortify" ];
}

