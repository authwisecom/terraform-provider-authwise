with import <nixpkgs> {};

stdenv.mkDerivation {

  name = "terraform-provider";

  buildInputs = with pkgs; [
    gnumake
    terraform
    go_1_21
  ];

  shellHook = ''
    export GOPATH=$HOME/go
    export PATH=$PATH:$HOME/go/bin
  '';

}

