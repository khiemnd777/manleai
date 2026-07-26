export type SchedulerEventPosition = {
  top: number;
  height: number;
  lane: number;
  laneCount: number;
};

export function schedulerEventPositionRule(className: string, position: SchedulerEventPosition): string {
  if (!/^scheduler-event-(day|week)-\d+-\d+$/.test(className)) {
    throw new Error("Scheduler event class is invalid.");
  }
  const top = nonNegative(position.top);
  const height = Math.max(1, nonNegative(position.height));
  const laneCount = Math.max(1, wholeNumber(position.laneCount));
  const lane = Math.min(Math.max(0, wholeNumber(position.lane)), laneCount - 1);
  const left = (lane / laneCount) * 100;
  const width = 100 / laneCount;
  return `.${className}{top:${decimal(top)}px;height:${decimal(height)}px;left:calc(${decimal(left)}% + 2px);width:calc(${decimal(width)}% - 4px)}`;
}

function nonNegative(value: number): number {
  return Number.isFinite(value) ? Math.max(0, value) : 0;
}

function wholeNumber(value: number): number {
  return Number.isFinite(value) ? Math.floor(value) : 0;
}

function decimal(value: number): string {
  return Number(value.toFixed(4)).toString();
}
