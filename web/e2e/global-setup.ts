import { buildE2EBinary } from "./server";

// Build the server binary once, before any worker starts one.
export default function globalSetup() {
	buildE2EBinary();
}
