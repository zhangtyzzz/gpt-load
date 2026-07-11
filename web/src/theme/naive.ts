import type { GlobalThemeOverrides } from "naive-ui";

const systemFont =
  "-apple-system, BlinkMacSystemFont, 'SF Pro Text', 'SF Pro Display', 'Segoe UI', sans-serif";

const sharedOverrides = {
  common: {
    primaryColor: "#0071e3",
    primaryColorHover: "#0077ed",
    primaryColorPressed: "#006edb",
    primaryColorSuppl: "#0a84ff",
    infoColor: "#0071e3",
    successColor: "#248a3d",
    warningColor: "#9a4f00",
    errorColor: "#d70015",
    borderRadius: "10px",
    borderRadiusSmall: "8px",
    fontFamily: systemFont,
  },
  Card: {
    paddingMedium: "20px",
    borderRadius: "16px",
  },
  Button: {
    fontWeight: "600",
    heightMedium: "38px",
    heightLarge: "46px",
    borderRadiusMedium: "10px",
    borderRadiusLarge: "12px",
  },
  Input: {
    heightMedium: "38px",
    heightLarge: "46px",
  },
  Menu: {
    itemHeight: "40px",
  },
  LoadingBar: {
    colorLoading: "#0071e3",
    colorError: "#d70015",
    height: "2px",
  },
} satisfies GlobalThemeOverrides;

export const lightThemeOverrides: GlobalThemeOverrides = sharedOverrides;

export const darkThemeOverrides: GlobalThemeOverrides = {
  ...sharedOverrides,
  common: {
    ...sharedOverrides.common,
    primaryColor: "#0a84ff",
    primaryColorHover: "#409cff",
    primaryColorPressed: "#0071e3",
    primaryColorSuppl: "#0a84ff",
    infoColor: "#0a84ff",
    successColor: "#30d158",
    warningColor: "#ffd60a",
    errorColor: "#ff453a",
    bodyColor: "#000000",
    cardColor: "#1c1c1e",
    modalColor: "#1c1c1e",
    popoverColor: "#2c2c2e",
    tableColor: "#1c1c1e",
    inputColor: "#2c2c2e",
    actionColor: "#2c2c2e",
    textColorBase: "#f5f5f7",
    textColor1: "#f5f5f7",
    textColor2: "#b5b5ba",
    textColor3: "#a1a1a6",
    borderColor: "rgba(255, 255, 255, 0.16)",
    dividerColor: "rgba(255, 255, 255, 0.12)",
  },
  Card: {
    ...sharedOverrides.Card,
    color: "#1c1c1e",
    textColor: "#f5f5f7",
    borderColor: "rgba(255, 255, 255, 0.12)",
  },
  Input: {
    ...sharedOverrides.Input,
    color: "#2c2c2e",
    textColor: "#f5f5f7",
    colorFocus: "#2c2c2e",
    borderHover: "rgba(10, 132, 255, 0.72)",
    borderFocus: "#0a84ff",
    placeholderColor: "#a1a1a6",
  },
  Select: {
    peers: {
      InternalSelection: {
        textColor: "#f5f5f7",
        color: "#2c2c2e",
        placeholderColor: "#a1a1a6",
      },
    },
  },
  DataTable: {
    tdColor: "#1c1c1e",
    thColor: "#2c2c2e",
    thTextColor: "#f5f5f7",
    tdTextColor: "#f5f5f7",
    borderColor: "rgba(255, 255, 255, 0.12)",
  },
  Tag: {
    textColor: "#f5f5f7",
  },
  Pagination: {
    itemTextColor: "#b5b5ba",
    itemTextColorActive: "#f5f5f7",
    itemColor: "#2c2c2e",
    itemColorActive: "#3a3a3c",
  },
  DatePicker: {
    itemTextColor: "#f5f5f7",
    itemColorActive: "#2c2c2e",
    panelColor: "#1c1c1e",
  },
  Message: {
    color: "#2c2c2e",
    textColor: "#f5f5f7",
    iconColor: "#f5f5f7",
    borderRadius: "12px",
    colorInfo: "#2c2c2e",
    colorSuccess: "#2c2c2e",
    colorWarning: "#2c2c2e",
    colorError: "#2c2c2e",
    colorLoading: "#2c2c2e",
  },
  LoadingBar: {
    ...sharedOverrides.LoadingBar,
    colorLoading: "#0a84ff",
    colorError: "#ff453a",
  },
  Notification: {
    color: "#2c2c2e",
    textColor: "#f5f5f7",
    titleTextColor: "#f5f5f7",
    descriptionTextColor: "#b5b5ba",
    borderRadius: "12px",
  },
};
