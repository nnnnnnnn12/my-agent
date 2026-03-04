package tools

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

type CalculatorTool struct{}

func NewCalculatorTool() *CalculatorTool {
	return &CalculatorTool{}
}

func (c *CalculatorTool) Name() string { return "calculator" }

func (c *CalculatorTool) Description() string {
	return "数学计算器，能计算加减乘除和括号表达式。当用户需要做数学计算时使用这个工具。"
}

func (c *CalculatorTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"expression": map[string]interface{}{
				"type":        "string",
				"description": "要计算的数学表达式，例如：2567 * 3891 或 (10 + 5) * 3",
			},
		},
		"required": []string{"expression"},
	}
}

func (c *CalculatorTool) Execute(args map[string]interface{}) (string, error) {
	expr, ok := args["expression"].(string)
	if !ok {
		return "", fmt.Errorf("缺少 expression 参数")
	}
	result, err := evaluate(expr)
	if err != nil {
		return "", fmt.Errorf("计算失败: %w", err)
	}
	// 修复：使用 %.0f 避免科学计数法
	return fmt.Sprintf("%s = %.0f", expr, result), nil
}

func evaluate(expr string) (float64, error) {
	expr = strings.ReplaceAll(expr, " ", "")
	val, _, err := parseExpr(expr, 0)
	return val, err
}

func parseExpr(expr string, pos int) (float64, int, error) {
	left, pos, err := parseTerm(expr, pos)
	if err != nil {
		return 0, pos, err
	}
	for pos < len(expr) {
		op := expr[pos]
		if op != '+' && op != '-' {
			break
		}
		pos++
		right, newPos, err := parseTerm(expr, pos)
		if err != nil {
			return 0, newPos, err
		}
		pos = newPos
		if op == '+' {
			left += right
		} else {
			left -= right
		}
	}
	return left, pos, nil
}

func parseTerm(expr string, pos int) (float64, int, error) {
	left, pos, err := parseFactor(expr, pos)
	if err != nil {
		return 0, pos, err
	}
	for pos < len(expr) {
		op := expr[pos]
		if op != '*' && op != '/' {
			break
		}
		pos++
		right, newPos, err := parseFactor(expr, pos)
		if err != nil {
			return 0, newPos, err
		}
		pos = newPos
		if op == '*' {
			left *= right
		} else {
			if right == 0 {
				return 0, pos, fmt.Errorf("除数不能为零")
			}
			left /= right
		}
	}
	return left, pos, nil
}

func parseFactor(expr string, pos int) (float64, int, error) {
	if pos >= len(expr) {
		return 0, pos, fmt.Errorf("表达式不完整")
	}
	if expr[pos] == '(' {
		pos++
		val, newPos, err := parseExpr(expr, pos)
		if err != nil {
			return 0, newPos, err
		}
		pos = newPos
		if pos >= len(expr) || expr[pos] != ')' {
			return 0, pos, fmt.Errorf("缺少右括号")
		}
		pos++
		return val, pos, nil
	}
	sign := 1.0
	if expr[pos] == '-' {
		sign = -1
		pos++
	}
	start := pos
	for pos < len(expr) && (unicode.IsDigit(rune(expr[pos])) || expr[pos] == '.') {
		pos++
	}
	if start == pos {
		return 0, pos, fmt.Errorf("位置 %d 处有无效字符", pos)
	}
	val, err := strconv.ParseFloat(expr[start:pos], 64)
	if err != nil {
		return 0, pos, fmt.Errorf("无法解析数字: %s", expr[start:pos])
	}
	return sign * val, pos, nil
}