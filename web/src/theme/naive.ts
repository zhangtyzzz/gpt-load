import type { GlobalThemeOverrides } from "naive-ui";

const systemFont =
  "-apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', sans-serif";

const sharedOverrides = {
  common: {
    primaryColor: "#ff4f1f",
    primaryColorHover: "#f04012",
    primaryColorPressed: "#d93a10",
    primaryColorSuppl: "#e84518",
    infoColor: "#377de2",
    successColor: "#1a9b55",
    warningColor: "#b87408",
    errorColor: "#dc3737",
    borderRadius: "10px",
    borderRadiusSmall: "6px",
    fontWeightStrong: "600",
    fontFamily: systemFont,
  },
  Card: {
    paddingMedium: "24px",
    borderRadius: "10px",
  },
  Button: {
    fontWeight: "500",
    heightMedium: "36px",
    heightLarge: "44px",
    borderRadiusMedium: "8px",
    borderRadiusLarge: "8px",
  },
  Input: {
    heightMedium: "36px",
    heightLarge: "44px",
  },
  Menu: {
    itemHeight: "40px",
    borderRadius: "6px",
  },
  DataTable: {
    thPaddingSmall: "8px 12px",
    tdPaddingSmall: "8px 12px",
  },
  Dialog: {
    borderRadius: "12px",
  },
  Tag: {
    borderRadius: "6px",
  },
  LoadingBar: {
    colorLoading: "#ff4f1f",
    colorError: "#dc3737",
    height: "3px",
  },
} satisfies GlobalThemeOverrides;

export const lightThemeOverrides: GlobalThemeOverrides = {
  ...sharedOverrides,
  common: {
    ...sharedOverrides.common,
    bodyColor: "#f7f7f5",
    cardColor: "#ffffff",
    modalColor: "#ffffff",
    popoverColor: "#ffffff",
    tableColor: "#ffffff",
    textColorBase: "#1c1c1b",
    textColor1: "#1c1c1b",
    textColor2: "#3c3c38",
    textColor3: "#6c6c67",
    borderColor: "#e4e4df",
    dividerColor: "#e4e4df",
  },
};

export const darkThemeOverrides: GlobalThemeOverrides = {
  ...sharedOverrides,
  common: {
    ...sharedOverrides.common,
    primaryColor: "#ff4f1f",
    primaryColorHover: "#f04012",
    primaryColorPressed: "#d93a10",
    primaryColorSuppl: "#ff845e",
    infoColor: "#89b9ff",
    successColor: "#68d99a",
    warningColor: "#f4bc52",
    errorColor: "#ff8a80",
    bodyColor: "#202020",
    cardColor: "#272727",
    modalColor: "#272727",
    popoverColor: "#2b2b29",
    tableColor: "#272727",
    inputColor: "#2b2b29",
    actionColor: "#2b2b29",
    textColorBase: "#f5f5f2",
    textColor1: "#f5f5f2",
    textColor2: "#d5d5d0",
    textColor3: "#b5b5b0",
    borderColor: "#40403c",
    dividerColor: "#40403c",
  },
  Card: {
    ...sharedOverrides.Card,
    color: "#272727",
    textColor: "#f5f5f2",
    borderColor: "#40403c",
  },
  Input: {
    ...sharedOverrides.Input,
    color: "#2b2b29",
    textColor: "#f5f5f2",
    colorFocus: "#2b2b29",
    borderHover: "rgba(255, 132, 94, 0.6)",
    borderFocus: "#ff845e",
    placeholderColor: "#999991",
  },
  Select: {
    peers: {
      InternalSelection: {
        textColor: "#f5f5f2",
        color: "#2b2b29",
        placeholderColor: "#999991",
      },
    },
  },
  DataTable: {
    tdColor: "#272727",
    thColor: "#232322",
    thTextColor: "#f5f5f2",
    tdTextColor: "#f5f5f2",
    borderColor: "#40403c",
  },
  Tag: {
    textColor: "#f5f5f2",
  },
  Pagination: {
    itemTextColor: "#b5b5b0",
    itemTextColorActive: "#f5f5f2",
    itemColor: "#2b2b29",
    itemColorActive: "#3a3a38",
  },
  DatePicker: {
    itemTextColor: "#f5f5f2",
    itemColorActive: "#2b2b29",
    panelColor: "#272727",
  },
  Message: {
    color: "#2b2b29",
    textColor: "#f5f5f2",
    iconColor: "#f5f5f2",
    borderRadius: "10px",
    colorInfo: "#2b2b29",
    colorSuccess: "#2b2b29",
    colorWarning: "#2b2b29",
    colorError: "#2b2b29",
    colorLoading: "#2b2b29",
  },
  LoadingBar: {
    ...sharedOverrides.LoadingBar,
    colorLoading: "#ff845e",
    colorError: "#ff8a80",
  },
  Notification: {
    color: "#2b2b29",
    textColor: "#f5f5f2",
    titleTextColor: "#f5f5f2",
    descriptionTextColor: "#b5b5b0",
    borderRadius: "10px",
  },
};
