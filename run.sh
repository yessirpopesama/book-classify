#!/bin/bash
# 运行图书分类程序

# 检查config.yaml是否存在
if [ ! -f "config.yaml" ]; then
    echo "错误: config.yaml文件不存在"
    echo "请先复制 config.yaml.example 为 config.yaml 并配置API密钥"
    exit 1
fi

# 运行程序（使用 . 来编译整个包，而不是单个文件）
go run .
