import Document, {
  Head,
  Html,
  Main,
  NextScript,
  type DocumentContext,
  type DocumentInitialProps,
} from "next/document";
import { Server } from "styletron-engine-atomic";
import { Provider } from "styletron-react";
import type { AppProps } from "next/app";
import type { ComponentType } from "react";
type Sheet = ReturnType<Server["getStylesheets"]>[number];
type Props = DocumentInitialProps & { sheets: Sheet[]; nonce?: string };
export default class AppDocument extends Document<Props> {
  static async getInitialProps(ctx: DocumentContext): Promise<Props> {
    const engine = new Server();
    const original = ctx.renderPage;
    ctx.renderPage = () =>
      original({
        enhanceApp: (App) => {
          const StyledApp = App as ComponentType<AppProps & { engine: Server }>;
          return function EnhancedApp(props) {
            return (
              <Provider value={engine}>
                <StyledApp {...props} engine={engine} />
              </Provider>
            );
          };
        },
      });
    const initial = await Document.getInitialProps(ctx);
    return {
      ...initial,
      sheets: engine.getStylesheets(),
      nonce: ctx.req?.headers["x-nonce"] as string | undefined,
    };
  }
  render() {
    return (
      <Html
        lang="en"
        className="text-zinc-950 antialiased lg:bg-zinc-100 dark:bg-zinc-900 dark:text-white dark:lg:bg-zinc-950"
      >
        <Head nonce={this.props.nonce}>
          {this.props.sheets.map((sheet, i) => (
            <style
              key={i}
              className="_styletron_hydrate_"
              {...sheet.attrs}
              dangerouslySetInnerHTML={{ __html: sheet.css }}
            />
          ))}
        </Head>
        <body>
          <Main />
          <NextScript nonce={this.props.nonce} />
        </body>
      </Html>
    );
  }
}
