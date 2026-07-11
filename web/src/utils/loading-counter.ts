export interface LoadingCounter {
  begin(): void;
  finish(): void;
  readonly pending: number;
}

export function createLoadingCounter(onActiveChange: (active: boolean) => void): LoadingCounter {
  let pending = 0;

  const notify = () => onActiveChange(pending > 0);

  return {
    begin() {
      pending += 1;
      notify();
    },
    finish() {
      pending = Math.max(0, pending - 1);
      notify();
    },
    get pending() {
      return pending;
    },
  };
}
