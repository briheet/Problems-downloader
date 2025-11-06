{ pkgs ? import <nixpkgs> {} }:

pkgs.mkShell {
  buildInputs = with pkgs; [
    go_1_25
    gopls
    golangci-lint-langserver
    delve

    gcc14
  ];
  shellHook = ''
    export NIX_CC="${pkgs.gcc14.cc}"
    export PATH="${pkgs.gcc14.cc}/bin:${pkgs.gcc14}/bin:$PATH"
    unset DEVELOPER_DIR_FOR_TARGET
  '';
}
