"use client";

import {useRef, useState} from "react";
import Monaco from "@monaco-editor/react";
import {editor} from "monaco-editor";
import axios from "axios";
import {createRoot} from "react-dom/client";
import {Drum, LoaderCircle} from "lucide-react";
import IOverlayWidget = editor.IOverlayWidget;

const rosePineDawnTheme: editor.IStandaloneThemeData = {
    base: "vs",
    inherit: false,
    rules: [
        {token: "", foreground: "575279", background: "faf4ed"},
        {token: "keyword", foreground: "d7827e"},
        {token: "string", foreground: "ea9d34"},
        {token: "number", foreground: "907aa9"},
        {token: "comment", foreground: "9893a5", fontStyle: "italic"},
        {token: "type", foreground: "56949f"},
        {token: "function", foreground: "286983"},
        {token: "variable", foreground: "575279"},
        {token: "constant", foreground: "b4637a"},
        {token: "delimiter", foreground: "9893a5"},
        {token: "delimiter.bracket", foreground: "9893a5"},
        {token: "delimiter.parenthesis", foreground: "9893a5"},
        {token: "delimiter.square", foreground: "9893a5"},
        {token: "delimiter.angle", foreground: "9893a5"},
        {token: "delimiter.curly", foreground: "9893a5"},
        {token: "punctuation", foreground: "9893a5"},
        {token: "punctuation.bracket", foreground: "9893a5"},
        {token: "punctuation.parenthesis", foreground: "9893a5"},
    ],
    colors: {
        "editor.background": "#faf4ed",
        "editor.foreground": "#575279",
        "editorBracketMatch.border": "#9893a5",
        "editorBracketMatch.background": "#f4ede8",
        "editor.lineHighlightBackground": "#f4ede8",
        "editorLineNumber.foreground": "#9893a5",
        "editorCursor.foreground": "#575279",
        "editorWhitespace.foreground": "#dfdad9",
        "editor.selectionBackground": "#dfdad9",
        "editor.inactiveSelectionBackground": "#f4ede8",
        "scrollbarSlider.background": "#f2e9e1",
        "scrollbarSlider.hoverBackground": "#dfdad9",
        "scrollbarSlider.activeBackground": "#cecacd",
        "sideBar.background": "#fffaf3",
        "sideBar.foreground": "#797593",
        "sideBarTitle.foreground": "#9893a5",
        "sideBarSectionHeader.background": "#f2e9e1",
        "sideBarSectionHeader.foreground": "#575279",

        "editorBracketHighlight.foreground1": "#9893a5",
        "editorBracketHighlight.foreground2": "#9893a5",
        "editorBracketHighlight.foreground3": "#9893a5",
        "editorBracketHighlight.foreground4": "#9893a5",
        "editorBracketHighlight.foreground5": "#9893a5",
        "editorBracketHighlight.foreground6": "#9893a5",
    },
};

interface ExecuteProps {
    execute: () => void;
}

const ExecuteButton = (params: ExecuteProps) => {
    const [isLoading, setIsLoading] = useState(false);


    const execute = async () => {
        setIsLoading(true);
        await params.execute();
        setIsLoading(false);

        console.log("hello")
    };

    return (
        <button onClick={execute} disabled={isLoading}>
            {isLoading ? (
                <LoaderCircle className="size-5 mt-5 animate-spin"/>
            ) : (
                <Drum className="size-5 mt-5"/>
            )}
        </button>
    );
}

class ToolboxElement implements IOverlayWidget {
    private readonly execute: () => void;

    constructor(execute: () => void) {
        this.execute = execute;
    }

    getDomNode(): HTMLElement {
        const div = document.createElement("div");
        const root = createRoot(div);
        root.render(
            <>
                <ExecuteButton execute={this.execute}/>
            </>,
        );
        return div;
    }

    getId(): string {
        return "toolbox";
    }

    getPosition(): editor.IOverlayWidgetPosition | null {
        return {
            preference: editor.OverlayWidgetPositionPreference.TOP_RIGHT_CORNER,
        };
    }
}

export default function Editor() {
    let editorRef = useRef<editor.IStandaloneCodeEditor | null>(null);

    const options: editor.IStandaloneEditorConstructionOptions = {
        minimap: {
            enabled: false,
        },
        renderLineHighlight: "none",
        theme: "rose-pine-dawn",
        fontSize: 16,
        bracketPairColorization: {
            enabled: false,
        },
        padding: {
            top: 16,
        },
        overviewRulerLanes: 0,
        hideCursorInOverviewRuler: true,
        overviewRulerBorder: false,
        scrollBeyondLastLine: false,
        automaticLayout: true,
        contextmenu: false,
    };

    const code = `package main\n\nimport (\n    "fmt"\n)\n\nfunc main() {\n    fmt.Println("Hello, world!")\n}`;

    const [output, setOutput] = useState("");

    async function execute() {
        setOutput("");
        const code = editorRef.current?.getValue();
        const resp = await axios.post("/api/execute", {code});
        setOutput(resp.data.output);
    }

    return (
        <div className="w-full h-full">
            <div className="w-full h-full relative rounded overflow-hidden">
                <Monaco
                    height="60vh"
                    options={options}
                    loading={null}
                    value={code}
                    language="go"
                    theme="rose-pine-dawn"
                    beforeMount={(monaco) => {
                        monaco.editor.defineTheme("rose-pine-dawn", rosePineDawnTheme);
                        monaco.editor.setTheme("rose-pine-dawn");
                    }}
                    onMount={(editor, monaco) => {
                        editor.addOverlayWidget(new ToolboxElement(execute));
                        editorRef.current = editor;
                    }}
                />
                <div className="absolute">hello</div>
            </div>
            <div className="whitespace-pre-line">{output}</div>
        </div>
    );
}
