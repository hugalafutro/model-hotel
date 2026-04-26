const THINKING_OPEN_RE = /<(?:thought|start_thought|think)>/g;
const THINKING_CLOSE_RE = /<\/(?:thought|end_thought|think)>/g;

export function extractThinking(raw: string): {
    thinking: string;
    content: string;
} {
    let content = raw;
    let thinking = "";

    const fenceMatch = content.match(/^<<\s*\n([\s\S]*?)\n>>\s*\n?/);
    if (fenceMatch) {
        thinking = fenceMatch[1].trim();
        content = content.slice(fenceMatch[0].length);
    }

    const tagOpen = content.search(/<(?:thought|start_thought|think)>/i);
    if (tagOpen !== -1) {
        const afterOpen = content.slice(tagOpen);
        const closeMatch = afterOpen.match(
            /<\/(?:thought|end_thought|think)>/i,
        );
        if (closeMatch) {
            const tagLen = afterOpen.indexOf(">");
            const closeEnd =
                afterOpen.indexOf(closeMatch[0]) + closeMatch[0].length;
            const inner = afterOpen.slice(
                tagLen + 1,
                afterOpen.indexOf(closeMatch[0]),
            );
            thinking = thinking ? thinking + "\n" + inner.trim() : inner.trim();
            content =
                content.slice(0, tagOpen) + content.slice(tagOpen + closeEnd);
        } else {
            const tagLen = afterOpen.indexOf(">");
            const inner = afterOpen.slice(tagLen + 1);
            thinking = thinking ? thinking + "\n" + inner.trim() : inner.trim();
            content = content.slice(0, tagOpen);
        }
    }

    content = content
        .replace(THINKING_OPEN_RE, "")
        .replace(THINKING_CLOSE_RE, "")
        .trimStart();

    return { thinking, content };
}