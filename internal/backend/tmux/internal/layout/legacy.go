package layout

import (
	"strconv"
	"strings"
)

type legacyParser struct {
	text   string
	offset int
}

func parseLegacy(text string) (cell, bool) {
	prefix, body, ok := strings.Cut(text, ",")
	if !ok || len(prefix) != 4 {
		return cell{}, false
	}

	expected, err := strconv.ParseUint(prefix, 16, 16)
	if err != nil || uint16(expected) != checksum(body) {
		return cell{}, false
	}

	p := legacyParser{text: body}
	root, ok := p.parseCell(0)
	return root, ok && p.offset == len(body)
}

func checksum(text string) uint16 {
	var sum uint16
	for i := range len(text) {
		sum = (sum >> 1) | (sum << 15)
		sum += uint16(text[i])
	}
	return sum
}

func (p *legacyParser) parseCell(depth int) (cell, bool) {
	if depth > maxDepth {
		return cell{}, false
	}

	c, ok := p.rectangle()
	if !ok || p.offset == len(p.text) {
		return cell{}, false
	}

	if p.take(',') {
		_, ok := p.number()
		return c, ok
	}

	open := p.text[p.offset]
	if open != '{' && open != '[' {
		return cell{}, false
	}
	p.offset++
	c.Kind = "h"
	close := byte('}')
	if open == '[' {
		c.Kind = "v"
		close = ']'
	}

	return p.children(c, close, depth)
}

func (p *legacyParser) rectangle() (cell, bool) {
	c := cell{Kind: "p"}
	for _, field := range []struct {
		target    **uint32
		separator byte
	}{
		{&c.Width, 'x'}, {&c.Height, ','}, {&c.X, ','}, {&c.Y, 0},
	} {
		value, ok := p.number()
		if !ok {
			return cell{}, false
		}
		*field.target = &value

		if field.separator != 0 && !p.take(field.separator) {
			return cell{}, false
		}
	}
	return c, *c.Width > 0 && *c.Height > 0
}

func (p *legacyParser) children(c cell, close byte, depth int) (cell, bool) {
	for {
		child, ok := p.parseCell(depth + 1)
		if !ok {
			return cell{}, false
		}
		c.Children = append(c.Children, child)

		if p.take(close) {
			return c, len(c.Children) >= 2
		}

		if !p.take(',') {
			return cell{}, false
		}
	}
}

func (p *legacyParser) number() (uint32, bool) {
	start := p.offset
	for p.offset < len(p.text) && p.text[p.offset] >= '0' && p.text[p.offset] <= '9' {
		p.offset++
	}
	value, err := strconv.ParseUint(p.text[start:p.offset], 10, 32)
	return uint32(value), err == nil
}

func (p *legacyParser) take(want byte) bool {
	if p.offset == len(p.text) || p.text[p.offset] != want {
		return false
	}
	p.offset++
	return true
}
