{ pkgs ? import <nixpkgs> {} }:

pkgs.mkShell {
  buildInputs = with pkgs; [
    go_1_25
    gopls
    golangci-lint-langserver
    delve

    gcc
  ];
  
}
