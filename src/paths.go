package main

// paths.go —— 版本号唯一来源。
//
// local profile 源已删除,不再有 baseDir / cccDir / profilesDir 之类的
// 路径常量;kdbx 源的路径集中在 src/kdbx.go (kdbxPath / credPathFor)。
//
// 编译时通过 -ldflags "-X main.version=..." 注入语义化版本号,
// 覆盖默认值 0.1.0-dev。build.ps1 在每次构建时自动注入。

// version 唯一来源。
var version = "0.1.0-dev"