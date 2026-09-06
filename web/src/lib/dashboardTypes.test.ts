import { describe, expect, it } from "vitest"
import { LAYOUT_VERSION, migrateLayout, type WidgetSpec } from "@/lib/dashboardTypes"

const widget: WidgetSpec = { id: "a", type: "fleet-overview", x: 0, y: 4, w: 12, h: 5 }

describe("migrateLayout", () => {
  it("doubles vertical measures for a pre-versioning layout", () => {
    expect(migrateLayout(undefined, [widget])).toEqual([{ ...widget, y: 8, h: 10 }])
  })

  it("leaves an already-current layout alone, and stays idempotent", () => {
    const once = migrateLayout(undefined, [widget])
    expect(migrateLayout(LAYOUT_VERSION, once)).toEqual(once)
  })
})
