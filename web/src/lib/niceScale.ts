/**
 * "Nice numbers" axis scaling — the same algorithm every serious charting
 * library (D3, Highcharts, Excel) uses to pick axis bounds/ticks: round to
 * 1/2/5×10^n rather than whatever the raw data min/max happen to be, so an
 * axis reads 0/5/10/15/20 GB instead of 0/4.7/9.3/14/18.6 GB. Source:
 * Paul Heckbert, "Nice Numbers for Graph Labels", Graphics Gems (1990).
 */

function niceNumber(range: number, round: boolean): number {
  if (range <= 0) return 1
  const exponent = Math.floor(Math.log10(range))
  const fraction = range / 10 ** exponent
  let niceFraction: number
  if (round) {
    if (fraction < 1.5) niceFraction = 1
    else if (fraction < 3) niceFraction = 2
    else if (fraction < 7) niceFraction = 5
    else niceFraction = 10
  } else {
    if (fraction <= 1) niceFraction = 1
    else if (fraction <= 2) niceFraction = 2
    else if (fraction <= 5) niceFraction = 5
    else niceFraction = 10
  }
  return niceFraction * 10 ** exponent
}

export interface NiceScale {
  min: number
  max: number
  ticks: number[]
}

/** Computes a rounded [min,max] and evenly-spaced tick values covering the
 * data range [dataMin, dataMax], aiming for roughly targetCount ticks. Always
 * anchors at 0 for a from-zero axis (every chart here is), so a flat/empty
 * series still gets a sane 0..step scale instead of a degenerate 0..0 one. */
export function computeNiceScale(dataMax: number, targetCount = 5): NiceScale {
  const max = Math.max(dataMax, 0)
  if (max === 0) {
    return { min: 0, max: 1, ticks: [0, 1] }
  }
  const range = niceNumber(max, false)
  const step = niceNumber(range / Math.max(1, targetCount - 1), true)
  const niceMax = Math.ceil(max / step) * step
  const ticks: number[] = []
  for (let v = 0; v <= niceMax + step / 2; v += step) ticks.push(Math.round(v * 1e6) / 1e6)
  return { min: 0, max: niceMax, ticks }
}
