import functions_framework


@functions_framework.cloud_event
def echo_event(cloud_event):
    print("gcp-relay delivered event:")
    print(f"  id={cloud_event['id']}")
    print(f"  type={cloud_event['type']}")
    print(f"  source={cloud_event['source']}")
    print(f"  data={cloud_event.data}")
