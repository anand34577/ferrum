declare module "@novnc/novnc/lib/rfb.js" {
  export type RFBCredentials = { username?: string; password?: string; target?: string }

  export default class RFB extends EventTarget {
    constructor(target: HTMLElement, urlOrChannel: string, options?: { credentials?: RFBCredentials; wsProtocols?: string[]; shared?: boolean })

    viewOnly: boolean
    scaleViewport: boolean
    clipViewport: boolean
    resizeSession: boolean
    showDotCursor: boolean
    background: string
    qualityLevel: number
    compressionLevel: number

    disconnect(): void
    sendCredentials(credentials: RFBCredentials): void
    sendCtrlAltDel(): void
    sendKey(keysym: number, code: string, down?: boolean): void
    focus(): void
    blur(): void
    clipboardPasteFrom(text: string): void
    machineShutdown(): void
    machineReboot(): void
    machineReset(): void
  }
}
